package app

import (
	"context"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// ListFxRatesQuery is issue #135's pure, stored-data-only FX read: it
// never calls Service.FxProvider (no network), only ever reads through
// ConvertAmount -> Service.FxRates.Lookup. This is what every "≈ N
// reporting-currency as of <date>" display in the UI is expected to call,
// including for a transaction amount the caller hasn't submitted (and so
// has nothing persisted anywhere to read from) -- there's still a rate to
// look up and apply, just not a stored transaction to read it off of.
//
// This is deliberately a thin wrapper over ConvertAmount, matching
// ConvertAmount's own doc comment ("a later issue's ListFxRates ... is
// expected to be a thin wrapper over the same query, not new logic"): it
// never re-derives a date or re-touches FxRates.Lookup itself.
type ListFxRatesQuery struct {
	// From is the ISO 4217 currency code to look up a rate for. Required.
	// When Amount is also set, Amount's own currency must equal From
	// (InvalidInput otherwise) -- From is not silently inferred from
	// Amount, so a caller always states the currency it's asking about
	// explicitly, whether or not it has an amount in hand yet.
	From string
	// To is the ISO 4217 currency code From is quoted against -- normally
	// the caller's own resolved reporting currency, though this query
	// doesn't resolve that itself, the same way AccountBalancesQuery's
	// TargetCurrency doesn't.
	To string
	// Policy selects which date is looked up -- see ConversionPolicy.
	// Required.
	Policy ConversionPolicy
	// TransactionDate is forwarded to ConvertAmountQuery unchanged.
	// Required when Policy is PolicyTransactionDate.
	TransactionDate string
	// PinnedDate is forwarded to ConvertAmountQuery unchanged. Required
	// when Policy is PolicyPinned.
	PinnedDate string
	// Amount, when non-nil, must be in currency From: it is converted into
	// To via ConvertAmount, and the result's Converted field is populated.
	// Leaving Amount nil still resolves and returns the rate itself
	// (Rate/RateDate/RateSource/Stale), just with no conversion performed
	// -- e.g. a bare "what's today's rate" display with no amount in
	// context yet.
	Amount *money.Money
	// AmountRaw is the caller-facing raw-string form of Amount, for a
	// surface (CLI, HTTP) that can't construct a money.Money itself --
	// those packages are barred from importing internal/domain (see
	// internal/lint's TestImportGraph), the same reason every other
	// user-typed amount in this package arrives as a raw string (e.g.
	// RecordTransferCommand.Amount) rather than a constructed Money.
	// Parsed against From via normalize.Amount, exactly like those. Only
	// consulted when Amount is nil; set at most one of the two.
	AmountRaw string
}

// ListFxRatesResult is ListFxRates' result: the resolved rate itself and
// its full ADR-0004 provenance (Rate, RateDate, RateSource, Stale, Policy
// -- the same fields ConvertedAmount carries, echoed here so a caller with
// no Amount still gets them), plus (only when the query's Amount was set)
// the converted figure.
type ListFxRatesResult struct {
	Rate       fx.Rate
	RateDate   domain.Date
	RateSource string
	Stale      bool
	Policy     ConversionPolicy
	// Converted holds the full converted figure when the query's Amount
	// was non-nil; nil otherwise.
	Converted *ConvertedAmount
}

// ListFxRates implements issue #135's read-only FX use case. It never
// calls Service.FxProvider -- viewing a rate must never itself trigger a
// fetch (ADR-0004) -- so a missing or provider-only rate surfaces exactly
// the *errs.Error ConvertAmount (via FxRates.Lookup) already returns for
// it, unchanged.
func (s *Service) ListFxRates(ctx context.Context, q ListFxRatesQuery) (ListFxRatesResult, error) {
	amount := q.Amount
	if amount == nil && q.AmountRaw != "" {
		minor, err := normalize.Amount(q.AmountRaw, q.From)
		if err != nil {
			return ListFxRatesResult{}, err
		}
		parsed, err := money.NewMoney(minor, q.From)
		if err != nil {
			return ListFxRatesResult{}, errs.New(errs.Internal).Wrap(err)
		}
		amount = &parsed
	}
	if amount != nil && amount.Currency() != q.From {
		return ListFxRatesResult{}, errs.New(errs.InvalidInput).
			Explain("The amount's currency (%s) does not match the requested From currency (%s).", amount.Currency(), q.From).
			Field("amount")
	}

	var lookupAmount money.Money
	if amount != nil {
		lookupAmount = *amount
	} else {
		zero, err := money.NewMoney(0, q.From)
		if err != nil {
			return ListFxRatesResult{}, errs.New(errs.InvalidInput).Explain("%q is not a known currency.", q.From).Field("from")
		}
		lookupAmount = zero
	}

	converted, err := s.ConvertAmount(ctx, ConvertAmountQuery{
		Amount:          lookupAmount,
		To:              q.To,
		Policy:          q.Policy,
		TransactionDate: q.TransactionDate,
		PinnedDate:      q.PinnedDate,
	})
	if err != nil {
		return ListFxRatesResult{}, err
	}

	result := ListFxRatesResult{
		Rate:       converted.Rate,
		RateDate:   converted.RateDate,
		RateSource: converted.RateSource,
		Stale:      converted.Stale,
		Policy:     converted.Policy,
	}
	if amount != nil {
		result.Converted = &converted
	}
	return result, nil
}
