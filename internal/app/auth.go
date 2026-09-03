// This file implements issue #55's application-layer auth use cases
// (ADR-0006): login, logout, password set, and API-token issue/list/revoke,
// plus the two entry points a future HTTP surface (issue #56) needs to turn
// a live session cookie or bearer token into an ActorID —
// AuthenticateSession and AuthenticateAPIToken.
//
// Design calls made here, where ADR-0006 and the issue leave room:
//
//   - There is no username or email column anywhere in the schema (only
//     internal/ports.SeededUserID's single well-known user), and building
//     one is explicitly out of this issue's scope — registration is
//     deliberately deferred (ADR-0006's "Deliberately not built"). Login
//     therefore takes only a password and verifies it against the single
//     seeded user. A LoginCommand.Username field that existed but drove no
//     real lookup would be exactly the "looks type-safe, guarantees
//     nothing" anti-pattern ADR-0005 warns command fields against — so it's
//     left off rather than added as a placeholder.
//   - Wrong password and "no password set yet" (a fresh install before the
//     first CLI password-set) both fail Login identically —
//     errs.Unauthenticated with the same generic message — so neither
//     leaks which case occurred, the same hygiene ADR-0006 asks for
//     credential handling generally.
//   - Session sliding-expiry duration (sessionTTL) isn't specified by
//     ADR-0006 beyond "sliding expiry" existing; 30 days is this package's
//     default until a real deployment asks for it to be configurable.
package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/platform/auth"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

const (
	// sessionTTL is how far a session's sliding expiry moves forward on
	// every successful use (ADR-0006's "sliding expiry"). See this file's
	// doc comment for why 30 days is the chosen default.
	sessionTTL = 30 * 24 * time.Hour
	// minPasswordLen is SetPassword's minimum accepted length. ADR-0006
	// specifies Argon2id hashing but no password policy; 8 is a
	// conservative floor rather than a full strength policy (entropy
	// meters, breach-list checks, and similar are real product features
	// deliberately not built here — see ADR-0006's "Deliberately not
	// built").
	minPasswordLen = 8
	// maxAPITokenNameLen mirrors maxAccountNameLen's cap for the same
	// reason: a user-supplied label, not a domain constraint, so a
	// generous but finite bound.
	maxAPITokenNameLen = 200
)

// loginFailureMessage is the one message both an unknown/password-less
// user and a genuinely wrong password produce externally, so neither case
// is distinguishable from outside (see this file's doc comment).
const loginFailureMessage = "Incorrect password."

// invalidCredentialMessage is what AuthenticateSession and
// AuthenticateAPIToken return for every way a presented credential can
// fail to authenticate — not found, expired, or revoked — for the same
// non-distinguishing reason loginFailureMessage exists.
const invalidCredentialMessage = "Invalid or expired credential."

// LoginCommand authenticates the single seeded user by password. See this
// file's doc comment for why there is no Username field.
type LoginCommand struct {
	Password string
}

// LoginResult is what a successful Login hands back to the surface that
// will hand it to a browser: ActorID and SessionID for the surface's own
// bookkeeping (a future Logout call needs SessionID), Token as the
// plaintext value to set in the session cookie — the only time this
// plaintext ever exists outside the browser — and ExpiresAt for the
// cookie's own expiry attribute.
type LoginResult struct {
	ActorID   string
	SessionID string
	Token     string
	ExpiresAt time.Time
}

// Login implements issue #55's login use case: verify the presented
// password against the single seeded user's stored hash, and on success,
// issue a new session.
func (s *Service) Login(ctx context.Context, cmd LoginCommand) (LoginResult, error) {
	user, err := s.Users.GetByID(ctx, ports.SeededUserID)
	if err != nil {
		if isNotFoundErr(err) {
			return LoginResult{}, loginFailure()
		}
		return LoginResult{}, err
	}
	if user.PasswordHash == nil {
		// No password has ever been set (a fresh install before the
		// first-run/CLI password set) — fails exactly like a wrong
		// password, not with a distinguishing message.
		return LoginResult{}, loginFailure()
	}

	ok, verr := auth.VerifyPassword(cmd.Password, *user.PasswordHash)
	if verr != nil {
		// A malformed stored hash is this package's own bug, not
		// anything the caller did wrong.
		return LoginResult{}, errs.New(errs.Internal).Wrap(verr)
	}
	if !ok {
		return LoginResult{}, loginFailure()
	}

	token, terr := auth.GenerateSessionToken()
	if terr != nil {
		return LoginResult{}, errs.New(errs.Internal).Wrap(terr)
	}

	now := s.Clock.Now()
	expiresAt := now.Add(sessionTTL)
	session := ports.Session{
		ID:         s.IDs.NewID(),
		UserID:     user.ID,
		TokenHash:  auth.HashSessionToken(token),
		CreatedAt:  now,
		LastUsedAt: now,
		ExpiresAt:  expiresAt,
	}
	if err := s.Sessions.Create(ctx, user.ID, session); err != nil {
		return LoginResult{}, err
	}

	return LoginResult{ActorID: user.ID, SessionID: session.ID, Token: token, ExpiresAt: expiresAt}, nil
}

// loginFailure is the one *errs.Error every Login failure path that must
// not leak which case occurred returns.
func loginFailure() error {
	return errs.New(errs.Unauthenticated).Explain(loginFailureMessage)
}

// LogoutCommand revokes one of the actor's own sessions.
type LogoutCommand struct {
	ActorID   string
	SessionID string
}

// Logout implements issue #55's logout use case. ADR-0006: session
// revocation is a hard delete, not a soft-revoke flag (unlike an API
// token) — SessionRepository.Delete does the actor-ownership check, so a
// SessionID that isn't actually cmd.ActorID's own comes back NotFound
// rather than deleting someone else's session.
func (s *Service) Logout(ctx context.Context, cmd LogoutCommand) error {
	if err := requireActorID(cmd.ActorID); err != nil {
		return err
	}
	if strings.TrimSpace(cmd.SessionID) == "" {
		return errs.New(errs.InvalidInput).Explain("A session is required.").Field("session_id")
	}
	return s.Sessions.Delete(ctx, cmd.ActorID, cmd.SessionID)
}

// SetPasswordCommand sets (or replaces) the acting user's password. Used
// both by a first-run/CLI password set and any future in-app "change
// password" flow — both are the same operation, overwrite the stored hash.
type SetPasswordCommand struct {
	ActorID     string
	NewPassword string
}

// SetPassword implements issue #55's password-set use case.
func (s *Service) SetPassword(ctx context.Context, cmd SetPasswordCommand) error {
	if err := requireActorID(cmd.ActorID); err != nil {
		return err
	}
	if len(cmd.NewPassword) < minPasswordLen {
		return errs.New(errs.InvalidInput).
			Explain("A password must be at least %d characters.", minPasswordLen).
			Field("new_password")
	}

	hash, err := auth.HashPassword(cmd.NewPassword, auth.DefaultArgon2Params)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return s.Users.SetPasswordHash(ctx, cmd.ActorID, hash)
}

// CreateAPITokenCommand issues a new named API token for the acting user.
// ExpiresAt follows every other date-shaped command field's convention
// (ADR-0005): a raw string ("", "today", "2027-01-01", ...), resolved
// through the injected clock and the user's timezone rather than typed,
// since a surface can't resolve it itself. "" means the token never
// expires.
type CreateAPITokenCommand struct {
	ActorID   string
	Name      string
	ExpiresAt string
}

// CreateAPITokenResult carries the plaintext token exactly once — this is
// the only point in the token's lifetime it's ever available outside the
// caller who receives this result; every later listing shows Token's
// TokenHash and nothing else.
type CreateAPITokenResult struct {
	Token          ports.APIToken
	PlaintextToken string
}

// CreateAPIToken implements issue #55's API-token issuance use case.
func (s *Service) CreateAPIToken(ctx context.Context, cmd CreateAPITokenCommand) (CreateAPITokenResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return CreateAPITokenResult{}, err
	}

	name, err := normalize.Text(cmd.Name, maxAPITokenNameLen)
	if err != nil {
		return CreateAPITokenResult{}, attachField(err, "name")
	}
	if name == "" {
		return CreateAPITokenResult{}, errs.New(errs.InvalidInput).Explain("A name is required.").Field("name")
	}

	expiresAtDate, err := optionalDate(cmd.ExpiresAt, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return CreateAPITokenResult{}, attachField(err, "expires_at")
	}
	var expiresAtPtr *time.Time
	if expiresAtDate != nil {
		instant, ierr := endOfDayInstant(*expiresAtDate, s.Config.UserTimezone)
		if ierr != nil {
			return CreateAPITokenResult{}, attachField(ierr, "expires_at")
		}
		expiresAtPtr = &instant
	}

	plaintext, terr := auth.GenerateAPIToken()
	if terr != nil {
		return CreateAPITokenResult{}, errs.New(errs.Internal).Wrap(terr)
	}

	token := ports.APIToken{
		ID:        s.IDs.NewID(),
		UserID:    cmd.ActorID,
		TokenHash: auth.HashAPIToken(plaintext),
		Name:      name,
		CreatedAt: s.Clock.Now(),
		ExpiresAt: expiresAtPtr,
	}
	if err := s.APITokens.Create(ctx, cmd.ActorID, token); err != nil {
		return CreateAPITokenResult{}, err
	}
	return CreateAPITokenResult{Token: token, PlaintextToken: plaintext}, nil
}

// ListAPITokensQuery lists every API token actorID owns (revoked and
// expired ones included, per APITokenRepository.List's contract — a
// management screen needs history, not just what's currently live).
type ListAPITokensQuery struct {
	ActorID string
}

// ListAPITokensResult is ListAPITokens' result.
type ListAPITokensResult struct {
	Tokens []ports.APIToken
}

// ListAPITokens implements issue #55's API-token listing use case.
func (s *Service) ListAPITokens(ctx context.Context, q ListAPITokensQuery) (ListAPITokensResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ListAPITokensResult{}, err
	}
	tokens, err := s.APITokens.List(ctx, q.ActorID)
	if err != nil {
		return ListAPITokensResult{}, err
	}
	return ListAPITokensResult{Tokens: tokens}, nil
}

// RevokeAPITokenCommand soft-revokes one of the actor's own API tokens.
type RevokeAPITokenCommand struct {
	ActorID string
	TokenID string
}

// RevokeAPIToken implements issue #55's API-token revocation use case.
// Authorisation — "can this actor revoke this token" — is
// APITokenRepository.Revoke's own actor-scoped WHERE clause: a TokenID
// that isn't cmd.ActorID's own comes back NotFound rather than revoking
// someone else's token, the same pattern every other repository method in
// this codebase already uses (e.g. AccountRepository.Update).
func (s *Service) RevokeAPIToken(ctx context.Context, cmd RevokeAPITokenCommand) error {
	if err := requireActorID(cmd.ActorID); err != nil {
		return err
	}
	if strings.TrimSpace(cmd.TokenID) == "" {
		return errs.New(errs.InvalidInput).Explain("A token is required.").Field("token_id")
	}
	return s.APITokens.Revoke(ctx, cmd.ActorID, cmd.TokenID, s.Clock.Now())
}

// AuthenticateSessionResult is what AuthenticateSession resolves a live
// session cookie into: ActorID for every other use case's ActorID field,
// SessionID for a subsequent Logout call, and ExpiresAt — the renewed
// sliding-expiry deadline — for the surface to reset the cookie's own
// expiry attribute to match.
type AuthenticateSessionResult struct {
	ActorID   string
	SessionID string
	ExpiresAt time.Time
}

// AuthenticateSession turns a plaintext session-cookie token into a
// resolved actor (ADR-0006: "middleware authenticates... hands that to the
// app layer"), sliding the session's expiry forward on every successful
// use per ADR-0006's sliding-expiry design. It is the entry point a future
// HTTP surface's session-cookie middleware calls; nothing about it is
// HTTP-specific.
//
// Every failure — an unrecognised token, one whose session has expired —
// returns the same errs.Unauthenticated with the same generic message, so
// a caller can't distinguish "no such session" from "session expired" from
// the response alone.
func (s *Service) AuthenticateSession(ctx context.Context, token string) (AuthenticateSessionResult, error) {
	if strings.TrimSpace(token) == "" {
		return AuthenticateSessionResult{}, invalidCredential()
	}

	session, err := s.Sessions.GetByTokenHash(ctx, auth.HashSessionToken(token))
	if err != nil {
		if isNotFoundErr(err) {
			return AuthenticateSessionResult{}, invalidCredential()
		}
		return AuthenticateSessionResult{}, err
	}

	now := s.Clock.Now()
	if !now.Before(session.ExpiresAt) {
		return AuthenticateSessionResult{}, invalidCredential()
	}

	expiresAt := now.Add(sessionTTL)
	if err := s.Sessions.Touch(ctx, session.UserID, session.ID, now, expiresAt); err != nil {
		return AuthenticateSessionResult{}, err
	}

	return AuthenticateSessionResult{ActorID: session.UserID, SessionID: session.ID, ExpiresAt: expiresAt}, nil
}

// AuthenticateAPITokenResult is what AuthenticateAPIToken resolves a
// bearer token into.
type AuthenticateAPITokenResult struct {
	ActorID string
	TokenID string
}

// AuthenticateAPIToken turns a plaintext bearer API token into a resolved
// actor, the API-token counterpart to AuthenticateSession. It rejects a
// revoked or expired token the same generic way — see
// AuthenticateSession's doc comment for why — and records the use via
// APITokenRepository.Touch on success (ADR-0006's "last used" bookkeeping;
// API tokens have no sliding expiry to renew, unlike a session).
func (s *Service) AuthenticateAPIToken(ctx context.Context, token string) (AuthenticateAPITokenResult, error) {
	if strings.TrimSpace(token) == "" {
		return AuthenticateAPITokenResult{}, invalidCredential()
	}

	apiToken, err := s.APITokens.GetByTokenHash(ctx, auth.HashAPIToken(token))
	if err != nil {
		if isNotFoundErr(err) {
			return AuthenticateAPITokenResult{}, invalidCredential()
		}
		return AuthenticateAPITokenResult{}, err
	}

	if apiToken.RevokedAt != nil {
		return AuthenticateAPITokenResult{}, invalidCredential()
	}
	now := s.Clock.Now()
	if apiToken.ExpiresAt != nil && !now.Before(*apiToken.ExpiresAt) {
		return AuthenticateAPITokenResult{}, invalidCredential()
	}

	if err := s.APITokens.Touch(ctx, apiToken.UserID, apiToken.ID, now); err != nil {
		return AuthenticateAPITokenResult{}, err
	}

	return AuthenticateAPITokenResult{ActorID: apiToken.UserID, TokenID: apiToken.ID}, nil
}

// invalidCredential is the one *errs.Error every credential-authentication
// failure in this file returns — see AuthenticateSession's doc comment for
// why unrecognised, expired, and revoked are all indistinguishable from
// outside.
func invalidCredential() error {
	return errs.New(errs.Unauthenticated).Explain(invalidCredentialMessage)
}

// isNotFoundErr reports whether err is an *errs.Error coded NotFound —
// used throughout this file to convert a bare "no such row" into the
// generic Unauthenticated failure a credential check must return instead,
// rather than leaking that the row genuinely doesn't exist.
func isNotFoundErr(err error) bool {
	var e *errs.Error
	return errors.As(err, &e) && e.Code == errs.NotFound
}

// endOfDayInstant converts d into the last instant of that calendar date in
// tz, expressed in UTC: an API token declared to expire "on 2027-01-01"
// stays valid through the end of that day in the user's own timezone, not
// at UTC midnight, which would cut the day short or long depending on the
// user's offset from UTC.
func endOfDayInstant(d domain.Date, tz string) (time.Time, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, errs.New(errs.InvalidInput).
			Explain("%q is not a known time zone", tz).
			Field("timezone").
			Wrap(err)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 999999999, loc).UTC(), nil
}
