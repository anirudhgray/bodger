package http_test

// Complements webui_test.go's package-internal TestSpaFileServer: that
// test proves spaFileServer's own fallback logic in isolation, this one
// proves NewServerHandler wires it in correctly — specifically that a
// real API route still wins over the web UI's "/" catch-all rather than
// being swallowed by it, which spaFileServer alone can't demonstrate
// since it never sees NewMux's routeTable (issue #58).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpsurface "github.com/anirudhgray/bodger/internal/surface/http"
)

func TestNewServerHandler_APIRoutesTakePrecedenceOverWebUI(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	srv := httptest.NewServer(httpsurface.NewServerHandler(svc, nil))
	t.Cleanup(srv.Close)

	t.Run("a real API route still returns JSON, not the web UI", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
	})

	t.Run("an unmatched path falls through to the web UI instead of a bare 404", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/transactions")
		if err != nil {
			t.Fatalf("GET /transactions: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (web UI's SPA fallback)", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
	})

	// Regression coverage for a real bug: GET /api/v1/healthz (routeTable
	// registers plain /healthz, not this) used to fall through to the web
	// UI catch-all and return a 200 with an HTML body — a client hitting a
	// slightly wrong API path got back a page it couldn't parse as JSON,
	// with no indication anything was wrong.
	t.Run("an unmatched path under /api/ gets a JSON 404, never the web UI", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/api/v1/healthz")
		if err != nil {
			t.Fatalf("GET /api/v1/healthz: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		var decoded struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if decoded.Error.Code != "not_found" {
			t.Errorf("error.code = %q, want \"not_found\"", decoded.Error.Code)
		}
	})
}
