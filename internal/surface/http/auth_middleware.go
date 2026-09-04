package http

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// contextKey namespaces this package's own context values so they can
// never collide with a key some other package might use on the same
// request context.
type contextKey int

const (
	actorIDContextKey contextKey = iota
	sessionIDContextKey
)

const (
	// sessionCookieName is the web UI's session cookie (ADR-0006).
	sessionCookieName = "bodger_session"
	// csrfHeaderName is the bodger-specific header a cookie-authenticated
	// state-changing request must carry (see requireAuth's doc comment).
	// Its value is never checked, only its presence: a cross-origin page
	// can set arbitrary form fields and even some headers, but browsers
	// don't let it set a custom header on a simple cross-origin request,
	// which is exactly the gap SameSite=Lax alone leaves open for a JSON
	// API (ADR-0006).
	csrfHeaderName = "X-Bodger-CSRF"
)

// withActor returns a copy of r whose context carries actorID and
// sessionID (sessionID is "" for a bearer-token-authenticated request,
// which has no session) — every handler in this package reads them back
// via actorID(r) (respond.go) and sessionID(r) below.
func withActor(r *http.Request, actorID, sessionID string) *http.Request {
	ctx := context.WithValue(r.Context(), actorIDContextKey, actorID)
	ctx = context.WithValue(ctx, sessionIDContextKey, sessionID)
	return r.WithContext(ctx)
}

// sessionID is the current request's session ID, set by requireAuth only
// when the request authenticated via a session cookie — "" for a
// bearer-token request, since an API token isn't a session. logoutHandler
// (auth.go) is the one caller that needs this: revoking "the session that
// made this request" only makes sense for a cookie-authenticated one.
func sessionID(r *http.Request) string {
	id, _ := r.Context().Value(sessionIDContextKey).(string)
	return id
}

// bearerToken extracts the plaintext token from an "Authorization: Bearer
// <token>" header, or reports ok=false if the header is absent or doesn't
// use the Bearer scheme.
func bearerToken(r *http.Request) (token string, ok bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token = strings.TrimSpace(strings.TrimPrefix(h, prefix))
	return token, token != ""
}

// isSafeMethod reports whether method is one CSRF protection exempts
// (ADR-0006: "reject cookie-authenticated state-changing requests" —
// GET/HEAD never change state).
func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// setSessionCookie sets the session cookie's value and expiry, per
// ADR-0006's credential table: HttpOnly, Secure, SameSite=Lax, sliding
// expiry.
func setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie expires the session cookie immediately — used on
// logout, and whenever a presented session cookie turns out to be invalid,
// so a browser stops resending a token that will never authenticate again.
func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// requireAuth wraps next so it only ever runs once the request's ActorID
// has been resolved from a real credential — a bearer API token
// (Authorization header) or a session cookie — and stashed on the
// request's context (withActor) for actorID(r) (respond.go) and
// sessionID(r) above to read back. This is authentication only, per
// ADR-0006: it establishes who is calling, and leaves every question of
// what they may do to the application-layer use case next eventually
// calls.
//
// A cookie-authenticated state-changing request (anything but GET/HEAD)
// additionally needs the csrfHeaderName header present — SameSite=Lax
// alone isn't a complete CSRF story for a JSON API (ADR-0006's own
// reasoning). A bearer-token request is exempt: a page a browser is
// tricked into submitting from can't be made to send an Authorization
// header at all, so there is no CSRF vector to close for it.
func (h *handlers) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token, ok := bearerToken(r); ok {
			result, err := h.svc.AuthenticateAPIToken(r.Context(), token)
			if err != nil {
				h.respondError(w, r, err)
				return
			}
			next(w, withActor(r, result.ActorID, ""))
			return
		}

		cookie, cerr := r.Cookie(sessionCookieName)
		if cerr != nil {
			h.respondError(w, r, errs.New(errs.Unauthenticated).Explain("Authentication is required."))
			return
		}

		result, aerr := h.svc.AuthenticateSession(r.Context(), cookie.Value)
		if aerr != nil {
			clearSessionCookie(w)
			h.respondError(w, r, aerr)
			return
		}

		if !isSafeMethod(r.Method) && r.Header.Get(csrfHeaderName) == "" {
			h.respondError(w, r, errs.New(errs.NotAllowed).
				Explain("This request needs the %s header.", csrfHeaderName))
			return
		}

		setSessionCookie(w, cookie.Value, result.ExpiresAt)
		next(w, withActor(r, result.ActorID, result.SessionID))
	}
}
