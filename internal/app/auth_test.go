package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// TestLogin_CorrectPassword covers issue #55's "correct login" done-when
// case: SetPassword followed by Login with that same password succeeds,
// resolves to the single seeded user, and hands back a token that itself
// authenticates via AuthenticateSession.
func TestLogin_CorrectPassword(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	if err := svc.SetPassword(ctx, app.SetPasswordCommand{ActorID: ports.SeededUserID, NewPassword: "correct-horse-battery"}); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	result, err := svc.Login(ctx, app.LoginCommand{Password: "correct-horse-battery"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if result.ActorID != ports.SeededUserID {
		t.Errorf("ActorID = %q, want %q", result.ActorID, ports.SeededUserID)
	}
	if result.Token == "" {
		t.Error("Token is empty, want a plaintext session token")
	}
	if result.SessionID == "" {
		t.Error("SessionID is empty")
	}
	if !result.ExpiresAt.After(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ExpiresAt = %v, want it after login time", result.ExpiresAt)
	}

	auth, err := svc.AuthenticateSession(ctx, result.Token)
	if err != nil {
		t.Fatalf("AuthenticateSession: %v", err)
	}
	if auth.ActorID != ports.SeededUserID {
		t.Errorf("AuthenticateSession ActorID = %q, want %q", auth.ActorID, ports.SeededUserID)
	}
	if auth.SessionID != result.SessionID {
		t.Errorf("AuthenticateSession SessionID = %q, want %q", auth.SessionID, result.SessionID)
	}
}

// TestLogin_WrongPassword covers issue #55's "wrong password" done-when
// case, and asserts the failure message matches TestLogin_NoPasswordSetYet's
// — the codebase's own hygiene choice that wrong-password and
// no-password-set-yet aren't distinguishable from outside.
func TestLogin_WrongPassword(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	if err := svc.SetPassword(ctx, app.SetPasswordCommand{ActorID: ports.SeededUserID, NewPassword: "correct-horse-battery"}); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	_, err := svc.Login(ctx, app.LoginCommand{Password: "wrong-password-entirely"})
	wantErrCode(t, err, errs.Unauthenticated)
}

// TestLogin_NoPasswordSetYet covers a fresh install (SeededUserID exists,
// but SetPassword has never been called) — Login must fail exactly like a
// wrong password, not with a message that reveals no password exists yet.
func TestLogin_NoPasswordSetYet(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")

	_, err := svc.Login(context.Background(), app.LoginCommand{Password: "anything"})
	wantErrCode(t, err, errs.Unauthenticated)
}

func TestSetPassword_RejectsShortPassword(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")

	err := svc.SetPassword(context.Background(), app.SetPasswordCommand{ActorID: ports.SeededUserID, NewPassword: "short"})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestSetPassword_RequiresActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")

	err := svc.SetPassword(context.Background(), app.SetPasswordCommand{NewPassword: "a-long-enough-password"})
	wantErrCode(t, err, errs.InvalidInput)
}

// TestLogout_DeletesSession covers logout: after Logout, the session's
// token no longer authenticates (ADR-0006: revocation is a hard delete).
func TestLogout_DeletesSession(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	if err := svc.SetPassword(ctx, app.SetPasswordCommand{ActorID: ports.SeededUserID, NewPassword: "correct-horse-battery"}); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	login, err := svc.Login(ctx, app.LoginCommand{Password: "correct-horse-battery"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if err := svc.Logout(ctx, app.LogoutCommand{ActorID: login.ActorID, SessionID: login.SessionID}); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	_, err = svc.AuthenticateSession(ctx, login.Token)
	wantErrCode(t, err, errs.Unauthenticated)
}

// TestAuthenticateSession_ExpiredSessionFails covers issue #55's "expired
// session" done-when case: a session whose sliding-expiry deadline has
// passed no longer authenticates, even though its token is otherwise
// valid and unrevoked.
func TestAuthenticateSession_ExpiredSessionFails(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	if err := svc.SetPassword(ctx, app.SetPasswordCommand{ActorID: ports.SeededUserID, NewPassword: "correct-horse-battery"}); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	login, err := svc.Login(ctx, app.LoginCommand{Password: "correct-horse-battery"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// Advance the frozen clock well past the session's sliding-expiry
	// deadline without ever calling AuthenticateSession in between, so
	// nothing renews it.
	advanceTestClock(t, svc, 31*24*time.Hour)

	_, err = svc.AuthenticateSession(ctx, login.Token)
	wantErrCode(t, err, errs.Unauthenticated)
}

// TestAuthenticateSession_SlidesExpiryForward covers ADR-0006's sliding
// expiry: a successful authentication moves ExpiresAt forward from "now",
// not from the original login time.
func TestAuthenticateSession_SlidesExpiryForward(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	if err := svc.SetPassword(ctx, app.SetPasswordCommand{ActorID: ports.SeededUserID, NewPassword: "correct-horse-battery"}); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	login, err := svc.Login(ctx, app.LoginCommand{Password: "correct-horse-battery"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	firstExpiry := login.ExpiresAt

	advanceTestClock(t, svc, 24*time.Hour)

	auth, err := svc.AuthenticateSession(ctx, login.Token)
	if err != nil {
		t.Fatalf("AuthenticateSession: %v", err)
	}
	if !auth.ExpiresAt.After(firstExpiry) {
		t.Errorf("ExpiresAt after renewal = %v, want it after the original %v", auth.ExpiresAt, firstExpiry)
	}
}

func TestAuthenticateSession_UnknownTokenFails(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")

	_, err := svc.AuthenticateSession(context.Background(), "not-a-real-token")
	wantErrCode(t, err, errs.Unauthenticated)
}

// TestCreateAndListAPIToken covers issuance and listing: the plaintext
// token is returned exactly once by CreateAPIToken, and a subsequent
// ListAPITokens shows the token's metadata without ever exposing that
// plaintext again.
func TestCreateAndListAPIToken(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	created, err := svc.CreateAPIToken(ctx, app.CreateAPITokenCommand{ActorID: testActorID, Name: "backup script"})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if created.PlaintextToken == "" {
		t.Error("PlaintextToken is empty, want the issued token")
	}
	if created.Token.Name != "backup script" {
		t.Errorf("Token.Name = %q, want %q", created.Token.Name, "backup script")
	}

	listed, err := svc.ListAPITokens(ctx, app.ListAPITokensQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListAPITokens: %v", err)
	}
	if len(listed.Tokens) != 1 || listed.Tokens[0].ID != created.Token.ID {
		t.Fatalf("ListAPITokens = %+v, want exactly the created token", listed.Tokens)
	}

	auth, err := svc.AuthenticateAPIToken(ctx, created.PlaintextToken)
	if err != nil {
		t.Fatalf("AuthenticateAPIToken: %v", err)
	}
	if auth.ActorID != testActorID {
		t.Errorf("AuthenticateAPIToken ActorID = %q, want %q", auth.ActorID, testActorID)
	}
	if auth.TokenID != created.Token.ID {
		t.Errorf("AuthenticateAPIToken TokenID = %q, want %q", auth.TokenID, created.Token.ID)
	}
}

func TestCreateAPIToken_RequiresName(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")

	_, err := svc.CreateAPIToken(context.Background(), app.CreateAPITokenCommand{ActorID: testActorID})
	wantErrCode(t, err, errs.InvalidInput)
}

// TestRevokeAPIToken_AuthenticateFailsAfterwards covers issue #55's
// "revoked token" done-when case: a revoked token's own plaintext no
// longer authenticates, even though it was never expired.
func TestRevokeAPIToken_AuthenticateFailsAfterwards(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	created, err := svc.CreateAPIToken(ctx, app.CreateAPITokenCommand{ActorID: testActorID, Name: "laptop"})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	if _, err := svc.AuthenticateAPIToken(ctx, created.PlaintextToken); err != nil {
		t.Fatalf("AuthenticateAPIToken before revoke: %v", err)
	}

	if err := svc.RevokeAPIToken(ctx, app.RevokeAPITokenCommand{ActorID: testActorID, TokenID: created.Token.ID}); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}

	_, err = svc.AuthenticateAPIToken(ctx, created.PlaintextToken)
	wantErrCode(t, err, errs.Unauthenticated)
}

// TestRevokeAPIToken_RefusesAnotherActorsToken covers this issue's
// authorisation-stays-in-the-app-layer requirement for API tokens: an
// actor may not revoke a token they don't own.
func TestRevokeAPIToken_RefusesAnotherActorsToken(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	created, err := svc.CreateAPIToken(ctx, app.CreateAPITokenCommand{ActorID: testActorID, Name: "laptop"})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}

	err = svc.RevokeAPIToken(ctx, app.RevokeAPITokenCommand{ActorID: "someone-else", TokenID: created.Token.ID})
	wantErrCode(t, err, errs.NotFound)

	// The token is still live, since the revoke above was refused.
	if _, err := svc.AuthenticateAPIToken(ctx, created.PlaintextToken); err != nil {
		t.Fatalf("AuthenticateAPIToken after refused cross-actor revoke: %v", err)
	}
}

// TestAuthenticateAPIToken_ExpiredFails covers an API token's optional
// expiry (ADR-0006): once ExpiresAt has passed, the token stops
// authenticating even though it was never explicitly revoked.
func TestAuthenticateAPIToken_ExpiredFails(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	created, err := svc.CreateAPIToken(ctx, app.CreateAPITokenCommand{
		ActorID: testActorID, Name: "expires today", ExpiresAt: "2026-01-01",
	})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if created.Token.ExpiresAt == nil {
		t.Fatal("Token.ExpiresAt is nil, want the resolved expiry instant")
	}

	// Still valid earlier the same day.
	if _, err := svc.AuthenticateAPIToken(ctx, created.PlaintextToken); err != nil {
		t.Fatalf("AuthenticateAPIToken before expiry: %v", err)
	}

	advanceTestClock(t, svc, 24*time.Hour)

	_, err = svc.AuthenticateAPIToken(ctx, created.PlaintextToken)
	wantErrCode(t, err, errs.Unauthenticated)
}

func TestAuthenticateAPIToken_UnknownTokenFails(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")

	_, err := svc.AuthenticateAPIToken(context.Background(), "bdg_not-a-real-token")
	wantErrCode(t, err, errs.Unauthenticated)
}

// TestActorResolution_SessionFeedsExistingUseCase is issue #55's
// actor-resolution done-when case: a session cookie authenticated via
// AuthenticateSession resolves to an ActorID that, fed into an existing
// pre-#55 use case (ListAccounts), correctly returns only that actor's own
// data — proving the resolved actor flows end to end, not just that
// AuthenticateSession returns a string.
func TestActorResolution_SessionFeedsExistingUseCase(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	// An account belonging to the seeded user, and one belonging to a
	// different actor entirely, so a resolved-actor leak would be
	// observable rather than accidentally invisible.
	if _, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID: ports.SeededUserID, Name: "HDFC Savings", Kind: "bank", Currency: "INR",
	}); err != nil {
		t.Fatalf("CreateAccount (seeded user): %v", err)
	}
	if _, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID: testActorID, Name: "Someone Else's Wallet", Kind: "cash", Currency: "USD",
	}); err != nil {
		t.Fatalf("CreateAccount (other actor): %v", err)
	}

	if err := svc.SetPassword(ctx, app.SetPasswordCommand{ActorID: ports.SeededUserID, NewPassword: "correct-horse-battery"}); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	login, err := svc.Login(ctx, app.LoginCommand{Password: "correct-horse-battery"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	resolved, err := svc.AuthenticateSession(ctx, login.Token)
	if err != nil {
		t.Fatalf("AuthenticateSession: %v", err)
	}

	// Feed the resolved ActorID — not ports.SeededUserID directly, and not
	// testActorID — into ListAccounts, a use case that predates issue #55
	// entirely.
	result, err := svc.ListAccounts(ctx, app.ListAccountsQuery{ActorID: resolved.ActorID})
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(result.Accounts) != 1 {
		t.Fatalf("ListAccounts returned %d accounts, want exactly 1 (the resolved actor's own)", len(result.Accounts))
	}
	if result.Accounts[0].Name() != "HDFC Savings" {
		t.Errorf("ListAccounts returned %q, want the resolved actor's own account", result.Accounts[0].Name())
	}
}

// advanceTestClock advances svc's injected clock, failing the test if svc
// wasn't built on a *clock.Frozen — every newTestService-built Service in
// this package is, so this is a small helper rather than a type assertion
// repeated at every call site.
func advanceTestClock(t *testing.T, svc *app.Service, d time.Duration) {
	t.Helper()
	frozen, ok := svc.Clock.(interface{ Advance(time.Duration) })
	if !ok {
		t.Fatalf("Service.Clock (%T) has no Advance method", svc.Clock)
	}
	frozen.Advance(d)
}
