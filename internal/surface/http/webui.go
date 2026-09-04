package http

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/webui"
)

// NewServerHandler wraps NewMux(svc, logger)'s REST API with the web UI's
// embedded static assets (issue #58): every request routeTable doesn't
// claim falls through to reserveAPIPrefix, so ServeCommand can hand one
// *http.Server both the API and the web UI without a second listener or
// process — ADR-0001. logger is passed straight through to NewMux so the
// API's respondError still logs an *errs.Error's cause chain the same way
// it does when NewMux is used directly (ADR-0011; issue #43), and so the
// same logging covers an unmatched /api/ path's synthesized 404 too.
//
// This is the only caller of internal/platform/webui in the codebase, and
// deliberately not something NewMux itself does: routeTable feeds
// openapi_gen.go's generator (issue #36), and static asset paths are not
// an API operation — folding them into routeTable would document them as
// one.
func NewServerHandler(svc *app.Service, logger *slog.Logger) http.Handler {
	h := &handlers{svc: svc, logger: logger}
	mux := NewMux(svc, logger)
	mux.Handle("/", reserveAPIPrefix(h, spaFileServer(webui.Dist())))
	return mux
}

// reserveAPIPrefix keeps the /api/ path prefix entirely for the REST API:
// a request under it that routeTable doesn't claim (a typo, an
// unsupported version, a path that used to exist) gets a real JSON 404
// through the same respondError every other API failure renders through,
// rather than silently falling through to webUI and returning 200 with
// an HTML body — which is exactly what GET /api/v1/healthz did before
// this existed, since routeTable registers plain /healthz, not
// /api/v1/healthz. Anything outside /api/ — a real client-side route, a
// bookmark, a typo'd page — still reaches webUI as the SPA fallback.
func reserveAPIPrefix(h *handlers, webUI http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.respondError(w, r, errs.New(errs.NotFound).Explain("No such API endpoint."))
			return
		}
		webUI.ServeHTTP(w, r)
	})
}

// spaFileServer serves webFS as static files, falling back to
// index.html — the client-rendered app shell — whenever the requested
// path isn't a real file in webFS. That fallback is what lets a hard
// refresh or a bookmarked link on a client-side route
// (web/src/routes.tsx's "/transactions", say) resolve to the app instead
// of a 404 from http.FileServer, which only ever sees the server's own
// routes.
func spaFileServer(webFS fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(webFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if rel == "" {
			rel = "."
		}

		if info, err := fs.Stat(webFS, rel); err != nil || info.IsDir() {
			// Not a real file webFS can serve directly (or it's a
			// directory, which http.FileServer would otherwise try to
			// list) — clone the request and rewrite its path to "/" so
			// http.FileServer serves index.html instead. Cloning, rather
			// than mutating r.URL.Path in place, follows the same
			// pattern net/http.StripPrefix uses so this handler never
			// surprises anything upstream still holding a reference to
			// the original request.
			cloned := *r
			u := *r.URL
			u.Path = "/"
			cloned.URL = &u
			fileServer.ServeHTTP(w, &cloned)
			return
		}

		fileServer.ServeHTTP(w, r)
	})
}
