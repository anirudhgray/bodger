package http_test

// Complements webui_test.go's package-internal TestSpaFileServer: that
// test proves spaFileServer's own fallback logic in isolation, this one
// proves NewServerHandler wires it in correctly — specifically that a
// real API route still wins over the web UI's "/" catch-all rather than
// being swallowed by it, which spaFileServer alone can't demonstrate
// since it never sees NewMux's routeTable (issue #58).

import (
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
}
