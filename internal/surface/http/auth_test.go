package http_test

// This file exercises issue #56's auth surface end to end: login sets a
// real HttpOnly session cookie, that cookie authenticates subsequent
// requests, and a cookie-authenticated state-changing request needs the
// CSRF header ADR-0006 calls for — bearer-token requests don't. It builds
// its own httptest.Server per test (via newTestService, not
// newTestServer) rather than reusing http_test.go's shared helper, since
// every test here needs to control exactly which credential a request
// presents.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
	httpsurface "github.com/anirudhgray/bodger/internal/surface/http"
)

// authTestServer is a bare NewMux server with no credential pre-wired —
// unlike http_test.go's testServer, every test below sets up its own.
func newAuthTestServer(t *testing.T) (*httptest.Server, *app.Service) {
	t.Helper()
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	srv := httptest.NewServer(httpsurface.NewMux(svc, nil))
	t.Cleanup(srv.Close)
	return srv, svc
}

// rawDo sends a request with no credential auto-attached — the raw
// counterpart to http_test.go's do, for tests that need to control
// exactly which cookie/header goes out.
func rawDo(t *testing.T, srv *httptest.Server, method, path string, body any, setup func(*http.Request)) (*http.Response, map[string]any) {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal(body): %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if setup != nil {
		setup(req)
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	var decoded map[string]any
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &decoded); err != nil {
			t.Fatalf("decode response body: %v", err)
		}
	}
	return resp, decoded
}

// sessionCookieFrom extracts the session cookie login just set, failing
// the test if there isn't one.
func sessionCookieFrom(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == "bodger_session" {
			return c
		}
	}
	t.Fatalf("no bodger_session cookie in response: %+v", resp.Cookies())
	return nil
}

func setUpPassword(t *testing.T, svc *app.Service, password string) {
	t.Helper()
	if err := svc.SetPassword(context.Background(), app.SetPasswordCommand{
		ActorID: ports.SeededUserID, NewPassword: password,
	}); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
}

func TestLogin_SetsSessionCookie_AndAuthenticatesSubsequentRequests(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	setUpPassword(t, svc, "correct-horse-battery")

	resp, decoded := rawDo(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]any{"password": "correct-horse-battery"}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200: %+v", resp.StatusCode, decoded)
	}
	data, _ := decoded["data"].(map[string]any)
	if data["actor_id"] != ports.SeededUserID {
		t.Errorf("login actor_id = %v, want %v", data["actor_id"], ports.SeededUserID)
	}
	cookie := sessionCookieFrom(t, resp)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie attributes = %+v, want HttpOnly+Secure+SameSite=Lax", cookie)
	}

	resp2, decoded2 := rawDo(t, srv, http.MethodGet, "/api/v1/accounts", nil, func(r *http.Request) {
		r.AddCookie(cookie)
	})
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("cookie-authenticated GET status = %d, want 200: %+v", resp2.StatusCode, decoded2)
	}
}

func TestLogin_WrongPassword_Returns401(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	setUpPassword(t, svc, "correct-horse-battery")

	resp, decoded := rawDo(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]any{"password": "nope"}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %+v", resp.StatusCode, decoded)
	}
}

func TestUnauthenticatedRequest_Returns401(t *testing.T) {
	srv, _ := newAuthTestServer(t)

	resp, decoded := rawDo(t, srv, http.MethodGet, "/api/v1/accounts", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %+v", resp.StatusCode, decoded)
	}
}

// TestCSRF_CookiePOSTWithoutHeaderIsRejected is the CSRF test issue #56
// calls for: a cross-origin-shaped cookie-authenticated POST with no
// bodger-specific header is rejected.
func TestCSRF_CookiePOSTWithoutHeaderIsRejected(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	setUpPassword(t, svc, "correct-horse-battery")
	loginResp, _ := rawDo(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]any{"password": "correct-horse-battery"}, nil)
	cookie := sessionCookieFrom(t, loginResp)

	resp, decoded := rawDo(t, srv, http.MethodPost, "/api/v1/accounts",
		map[string]any{"name": "CSRF Victim", "type": "cash", "currency": "USD"},
		func(r *http.Request) { r.AddCookie(cookie) },
	)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %+v", resp.StatusCode, decoded)
	}
	errObj, _ := decoded["error"].(map[string]any)
	if errObj["code"] != string(errs.NotAllowed) {
		t.Errorf("error code = %v, want %q", errObj["code"], errs.NotAllowed)
	}
}

// TestCSRF_CookiePOSTWithHeaderSucceeds proves the same request succeeds
// once the CSRF header is present.
func TestCSRF_CookiePOSTWithHeaderSucceeds(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	setUpPassword(t, svc, "correct-horse-battery")
	loginResp, _ := rawDo(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]any{"password": "correct-horse-battery"}, nil)
	cookie := sessionCookieFrom(t, loginResp)

	resp, decoded := rawDo(t, srv, http.MethodPost, "/api/v1/accounts",
		map[string]any{"name": "Legit Request", "type": "cash", "currency": "USD"},
		func(r *http.Request) {
			r.AddCookie(cookie)
			r.Header.Set("X-Bodger-CSRF", "1")
		},
	)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %+v", resp.StatusCode, decoded)
	}
}

// TestCSRF_BearerTokenPOSTIsExempt proves a bearer-token request needs no
// CSRF header at all — see auth_middleware.go's requireAuth doc comment
// for why.
func TestCSRF_BearerTokenPOSTIsExempt(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	tok, err := svc.CreateAPIToken(context.Background(), app.CreateAPITokenCommand{ActorID: ports.SeededUserID, Name: "script"})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	resp, decoded := rawDo(t, srv, http.MethodPost, "/api/v1/accounts",
		map[string]any{"name": "Bearer Request", "type": "cash", "currency": "USD"},
		func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok.PlaintextToken) },
	)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %+v", resp.StatusCode, decoded)
	}
}

func TestLogout_RevokesSessionCookie(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	setUpPassword(t, svc, "correct-horse-battery")
	loginResp, _ := rawDo(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]any{"password": "correct-horse-battery"}, nil)
	cookie := sessionCookieFrom(t, loginResp)

	resp, decoded := rawDo(t, srv, http.MethodPost, "/api/v1/auth/logout", nil, func(r *http.Request) {
		r.AddCookie(cookie)
		r.Header.Set("X-Bodger-CSRF", "1")
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200: %+v", resp.StatusCode, decoded)
	}

	resp2, decoded2 := rawDo(t, srv, http.MethodGet, "/api/v1/accounts", nil, func(r *http.Request) {
		r.AddCookie(cookie)
	})
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("post-logout status = %d, want 401: %+v", resp2.StatusCode, decoded2)
	}
}

func TestLogout_WithBearerTokenOnly_RejectsWithInvalidInput(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	tok, err := svc.CreateAPIToken(context.Background(), app.CreateAPITokenCommand{ActorID: ports.SeededUserID, Name: "script"})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	resp, decoded := rawDo(t, srv, http.MethodPost, "/api/v1/auth/logout", nil, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+tok.PlaintextToken)
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %+v", resp.StatusCode, decoded)
	}
}

// TestLogoutAll_RevokesEverySession covers issue #71's "log out
// everywhere" over HTTP: a second, independent session also stops
// authenticating.
func TestLogoutAll_RevokesEverySession(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	setUpPassword(t, svc, "correct-horse-battery")

	firstResp, _ := rawDo(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]any{"password": "correct-horse-battery"}, nil)
	first := sessionCookieFrom(t, firstResp)
	secondResp, _ := rawDo(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]any{"password": "correct-horse-battery"}, nil)
	second := sessionCookieFrom(t, secondResp)

	resp, decoded := rawDo(t, srv, http.MethodPost, "/api/v1/auth/logout-all", nil, func(r *http.Request) {
		r.AddCookie(first)
		r.Header.Set("X-Bodger-CSRF", "1")
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout-all status = %d, want 200: %+v", resp.StatusCode, decoded)
	}

	resp2, decoded2 := rawDo(t, srv, http.MethodGet, "/api/v1/accounts", nil, func(r *http.Request) {
		r.AddCookie(second)
	})
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second session status after logout-all = %d, want 401: %+v", resp2.StatusCode, decoded2)
	}
}

// TestAPITokenLifecycle_OverHTTP covers create/list/revoke end to end,
// through the real routes rather than the application layer directly.
func TestAPITokenLifecycle_OverHTTP(t *testing.T) {
	srv, svc := newAuthTestServer(t)
	setUpPassword(t, svc, "correct-horse-battery")
	loginResp, _ := rawDo(t, srv, http.MethodPost, "/api/v1/auth/login", map[string]any{"password": "correct-horse-battery"}, nil)
	cookie := sessionCookieFrom(t, loginResp)
	authed := func(r *http.Request) { r.AddCookie(cookie); r.Header.Set("X-Bodger-CSRF", "1") }

	resp, decoded := rawDo(t, srv, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"name": "laptop"}, authed)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %+v", resp.StatusCode, decoded)
	}
	created, _ := decoded["data"].(map[string]any)
	tokenID, _ := created["id"].(string)
	plaintext, _ := created["token"].(string)
	if tokenID == "" || plaintext == "" {
		t.Fatalf("created token missing id or plaintext: %+v", created)
	}

	listResp, listDecoded := rawDo(t, srv, http.MethodGet, "/api/v1/auth/tokens", nil, authed)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %+v", listResp.StatusCode, listDecoded)
	}
	list, _ := listDecoded["data"].([]any)
	if len(list) != 1 {
		t.Fatalf("list = %+v, want exactly 1 token", list)
	}

	// The freshly minted token itself authenticates a request.
	useResp, useDecoded := rawDo(t, srv, http.MethodGet, "/api/v1/accounts", nil, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+plaintext)
	})
	if useResp.StatusCode != http.StatusOK {
		t.Fatalf("bearer-authenticated request status = %d, want 200: %+v", useResp.StatusCode, useDecoded)
	}

	revokeResp, revokeDecoded := rawDo(t, srv, http.MethodDelete, "/api/v1/auth/tokens/"+tokenID, nil, authed)
	if revokeResp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200: %+v", revokeResp.StatusCode, revokeDecoded)
	}

	postRevokeResp, postRevokeDecoded := rawDo(t, srv, http.MethodGet, "/api/v1/accounts", nil, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+plaintext)
	})
	if postRevokeResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("post-revoke status = %d, want 401: %+v", postRevokeResp.StatusCode, postRevokeDecoded)
	}
}
