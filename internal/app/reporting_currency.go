package app

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// GetReportingCurrency returns actorID's configured reporting currency —
// ADR-0004's currency precedence ladder's "user" rung
// (internal/app/normalize.Currency) — or "" if actorID has never set one.
// An unset reporting currency is a normal, expected state (a fresh
// install's default), not an error: normalize.Currency already treats ""
// as "this level has no opinion" and falls through to the instance
// default, exactly as it did before this field existed.
func (s *Service) GetReportingCurrency(ctx context.Context, actorID string) (string, error) {
	if err := requireActorID(actorID); err != nil {
		return "", err
	}
	user, err := s.Users.GetByID(ctx, actorID)
	if err != nil {
		return "", err
	}
	if user.ReportingCurrency == nil {
		return "", nil
	}
	return *user.ReportingCurrency, nil
}

// SetReportingCurrency sets (or replaces) actorID's reporting currency.
// currency is validated against money's reference data the same way
// normalize.Currency validates every other level of its ladder, so a
// caller can never persist a code the ladder would later reject.
func (s *Service) SetReportingCurrency(ctx context.Context, actorID, currency string) error {
	if err := requireActorID(actorID); err != nil {
		return err
	}
	if _, ok := money.LookupCurrency(currency); !ok {
		return errs.New(errs.InvalidInput).
			Explain("%q is not a known currency", currency).
			Field("currency")
	}
	return s.Users.SetReportingCurrency(ctx, actorID, currency)
}

// resolveReportingCurrency is GetReportingCurrency with one difference: a
// NotFound actor resolves to "" (no opinion) instead of propagating the
// error. It's what CreateAccount and buildOutflowOrInflowPosting call to
// fill in ADR-0004's ladder's "user" rung — internal, best-effort
// personalisation of a value the ladder already knows how to skip over,
// not an authorisation check (that's resolveOwnedAccount/resolveOwnedCategory's
// job, decided separately and earlier in every caller that has one). A
// real actor always has a user row (ADR-0006: every actor is the single
// seeded user), so NotFound here can only happen for a synthetic actor
// nothing in this codebase treats as requiring one - CreateAccount itself
// has never checked that its ActorID names a real user, and this must not
// be the place that starts.
func (s *Service) resolveReportingCurrency(ctx context.Context, actorID string) (string, error) {
	currency, err := s.GetReportingCurrency(ctx, actorID)
	if err != nil {
		if isNotFoundErr(err) {
			return "", nil
		}
		return "", err
	}
	return currency, nil
}
