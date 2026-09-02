package http

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/webui"
)

// NewServerHandler wraps NewMux(svc, logger)'s REST API with the web UI's
// embedded static assets (issue #58): every request routeTable doesn't
// claim falls through to webui.Dist(), so ServeCommand can hand one
// *http.Server both the API and the web UI without a second listener or
// process — ADR-0001. logger is passed straight through to NewMux so the
// API's respondError still logs an *errs.Error's cause chain the same way
// it does when NewMux is used directly (ADR-0011; issue #43) — the web UI
// catch-all itself never produces an *errs.Error, so it has nothing of
// its own to log.
//
// This is the only caller of internal/platform/webui in the codebase, and
// deliberately not something NewMux itself does: routeTable feeds
// openapi_gen.go's generator (issue #36), and static asset paths are not
// an API operation — folding them into routeTable would document them as
// one.
func NewServerHandler(svc *app.Service, logger *slog.Logger) http.Handler {
	mux := NewMux(svc, logger)
	mux.Handle("/", spaFileServer(webui.Dist()))
	return mux
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
