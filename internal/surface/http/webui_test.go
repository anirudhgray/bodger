package http

// This file is package http, not http_test, deliberately: spaFileServer
// (webui.go) is unexported, and this test exercises it directly against a
// fake filesystem rather than through internal/platform/webui's real
// embedded assets, so it doesn't depend on whether `make build-web` has
// run in this checkout (issue #58).

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSpaFileServer(t *testing.T) {
	webFS := fstest.MapFS{
		"index.html":    {Data: []byte("<html>app shell</html>")},
		"assets/app.js": {Data: []byte("console.log('hi')")},
	}
	handler := spaFileServer(webFS)

	tests := []struct {
		name string
		path string
		want string
	}{
		{"serves a real asset directly", "/assets/app.js", "console.log('hi')"},
		{"serves index.html at root", "/", "<html>app shell</html>"},
		{"falls back to index.html for a client-side route", "/transactions", "<html>app shell</html>"},
		{"falls back to index.html for a nested client-side route", "/transactions/new", "<html>app shell</html>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: status = %d, want 200", tt.path, rec.Code)
			}
			if got := rec.Body.String(); got != tt.want {
				t.Errorf("GET %s: body = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
