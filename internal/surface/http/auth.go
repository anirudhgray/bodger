package http

import (
	"net/http"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// loginRequest is POST /api/v1/auth/login's request body. There is no
// username field — see internal/app/auth.go's doc comment for why: the
// single seeded user is authenticated by password alone.
type loginRequest struct {
	Password string `json:"password"`
}

// authView is what a successful login hands back in the response body —
// deliberately minimal, since the session credential itself travels as an
// HttpOnly cookie the caller never reads directly (ADR-0006).
type authView struct {
	ActorID string `json:"actor_id"`
}

// okView is the response body for a route with nothing further to report
// beyond succeeding (logout, logout-all, revoke) — a real, named response
// type (an empty struct confuses openapi3gen's schema naming) rather than
// a nil Response, matching every other route in this package's routeTable
// in always naming a real response type for openapi_gen.go to reflect
// over.
type okView struct {
	OK bool `json:"ok"`
}

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	result, err := h.svc.Login(r.Context(), app.LoginCommand{Password: body.Password})
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	setSessionCookie(r, w, result.Token, result.ExpiresAt)
	respond(w, http.StatusOK, authView{ActorID: result.ActorID})
}

// logout revokes the session that authenticated this request. Only a
// cookie-authenticated request has one — see sessionID's doc comment
// (auth_middleware.go) — a bearer-token request reaching this route gets
// InvalidInput rather than silently doing nothing.
func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	sid := sessionID(r)
	if sid == "" {
		h.respondError(w, r, errs.New(errs.InvalidInput).
			Explain("Logging out requires a session; revoke an API token instead."))
		return
	}

	if err := h.svc.Logout(r.Context(), app.LogoutCommand{ActorID: actorID(r), SessionID: sid}); err != nil {
		h.respondError(w, r, err)
		return
	}
	clearSessionCookie(r, w)
	respond(w, http.StatusOK, okView{OK: true})
}

// logoutAll revokes every session the acting user owns (issue #71),
// including whichever one made this request, if any — the same
// "everything, no exceptions" behaviour SetPassword's own session
// revocation follows (internal/app/auth.go's doc comment).
func (h *handlers) logoutAll(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.LogoutAllSessions(r.Context(), app.LogoutAllSessionsCommand{ActorID: actorID(r)}); err != nil {
		h.respondError(w, r, err)
		return
	}
	if sessionID(r) != "" {
		clearSessionCookie(r, w)
	}
	respond(w, http.StatusOK, okView{OK: true})
}

// changePasswordRequest is POST /api/v1/auth/password's request body.
type changePasswordRequest struct {
	NewPassword string `json:"new_password"`
}

// changePassword implements issue #62's in-app password change, over the
// same app.SetPassword use case issue #57's CLI set-password command
// calls — same operation, different surface. SetPassword revokes every
// session the actor owns, including whichever one made this request (see
// internal/app/auth.go's doc comment), so this clears the caller's own
// cookie the same way logoutAll does: the request that changed the
// password is itself no longer authenticated afterward, by design.
func (h *handlers) changePassword(w http.ResponseWriter, r *http.Request) {
	var body changePasswordRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	if err := h.svc.SetPassword(r.Context(), app.SetPasswordCommand{
		ActorID: actorID(r), NewPassword: body.NewPassword,
	}); err != nil {
		h.respondError(w, r, err)
		return
	}
	if sessionID(r) != "" {
		clearSessionCookie(r, w)
	}
	respond(w, http.StatusOK, okView{OK: true})
}

// apiTokenView is the JSON shape of an API token. There is no token_hash
// field — the hash is never rendered back, and the plaintext only ever
// appears once, on createAPIToken's response (below).
type apiTokenView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at" format:"date-time"`
	LastUsedAt string `json:"last_used_at,omitempty" doc:"Absent if the token has never been used." format:"date-time"`
	ExpiresAt  string `json:"expires_at,omitempty" doc:"Absent for a token that doesn't expire." format:"date-time"`
	RevokedAt  string `json:"revoked_at,omitempty" doc:"Absent for a token that's still live." format:"date-time"`
}

func apiTokenViewFrom(t ports.APIToken) apiTokenView {
	v := apiTokenView{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: t.CreatedAt.Format(time.RFC3339),
	}
	if t.LastUsedAt != nil {
		v.LastUsedAt = t.LastUsedAt.Format(time.RFC3339)
	}
	if t.ExpiresAt != nil {
		v.ExpiresAt = t.ExpiresAt.Format(time.RFC3339)
	}
	if t.RevokedAt != nil {
		v.RevokedAt = t.RevokedAt.Format(time.RFC3339)
	}
	return v
}

// createAPITokenRequest is POST /api/v1/auth/tokens' request body.
type createAPITokenRequest struct {
	Name      string `json:"name"`
	ExpiresAt string `json:"expires_at,omitempty" doc:"Omit for a token that never expires." format:"date"`
}

// createAPITokenResponse is createAPIToken's response: apiTokenView's
// fields plus the plaintext token, present exactly once, here and never
// again (see app.CreateAPITokenResult's doc comment).
type createAPITokenResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at" format:"date-time"`
	ExpiresAt string `json:"expires_at,omitempty" doc:"Absent for a token that doesn't expire." format:"date-time"`
	Token     string `json:"token" doc:"The plaintext token. Shown only this once - store it now; it can't be retrieved again."`
}

func (h *handlers) createAPIToken(w http.ResponseWriter, r *http.Request) {
	var body createAPITokenRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	result, err := h.svc.CreateAPIToken(r.Context(), app.CreateAPITokenCommand{
		ActorID: actorID(r), Name: body.Name, ExpiresAt: body.ExpiresAt,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	view := apiTokenViewFrom(result.Token)
	respond(w, http.StatusCreated, createAPITokenResponse{
		ID: view.ID, Name: view.Name, CreatedAt: view.CreatedAt, ExpiresAt: view.ExpiresAt,
		Token: result.PlaintextToken,
	})
}

func (h *handlers) listAPITokens(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ListAPITokens(r.Context(), app.ListAPITokensQuery{ActorID: actorID(r)})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	views := make([]apiTokenView, 0, len(result.Tokens))
	for _, t := range result.Tokens {
		views = append(views, apiTokenViewFrom(t))
	}
	respond(w, http.StatusOK, views)
}

func (h *handlers) revokeAPIToken(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RevokeAPIToken(r.Context(), app.RevokeAPITokenCommand{
		ActorID: actorID(r), TokenID: r.PathValue("id"),
	}); err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, okView{OK: true})
}
