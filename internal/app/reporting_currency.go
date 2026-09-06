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
// fill in ADR-0004's ladder's "user" rung.
//
// Why swallow NotFound: CreateAccount and buildOutflowOrInflowPosting have
// never required ActorID to name a real row in users — only that it owns
// whatever account/category is being touched, which
// resolveOwnedAccount/resolveOwnedCategory already check separately and
// earlier. Several existing tests rely on this: they call CreateAccount
// with a synthetic ActorID like "someone-else" that has no corresponding
// users row (see e.g. accounts_test.go's cross-actor-isolation tests).
// Looking up a reporting-currency preference is pure personalisation on
// top of that; if it errored for an actor nothing has ever required to
// exist, CreateAccount would start failing for callers it used to accept
// — a regression this feature has no business introducing. So a
// not-found actor is treated exactly like a real actor who simply hasn't
// set a reporting currency: "" falls through to the instance default via
// normalize.Currency's ladder, unchanged from before this field existed.
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
