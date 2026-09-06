package app

import (
	"context"
	"strings"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// ConversionPolicy names one of ADR-0004's three conversion policies. Which
// date SelectRate's `want` argument resolves to depends entirely on which
// policy is chosen -- it is never "today" except under PolicyCurrent. A
// per-row conversion (e.g. a transaction list spanning many historical
// dates) is expected to call ConvertAmount once per row, each with that
// row's own date, rather than resolving one date and reusing it.
type ConversionPolicy string

const (
	// PolicyTransactionDate looks up the rate on the specific transaction's
	// own booked date (ConvertAmountQuery.TransactionDate) -- ADR-0004's
	// default for historical reports: reproducible, since a rate already
	// recorded for a past date never changes.
	PolicyTransactionDate ConversionPolicy = "transaction_date"
	// PolicyCurrent looks up the rate on today's date, resolved through the
	// service's clock (never time.Now() directly) -- ADR-0004's default
	// for present-tense questions: current balances, net worth.
	PolicyCurrent ConversionPolicy = "current"
	// PolicyPinned looks up the rate on one caller-chosen date
	// (ConvertAmountQuery.PinnedDate) -- for comparing periods on a common
	// basis.
	PolicyPinned ConversionPolicy = "pinned"
)

// ConvertAmountQuery is ADR-0004's single policy-aware currency-conversion
// query -- the one place this logic lives. AccountBalances calls it below
// rather than duplicating any of it, and a later issue's ListFxRates
// (ad-hoc single-amount conversion) is expected to be a thin wrapper over
// the same query, not new logic.
//
// This is a pure read over already-stored rates (via
// Service.FxRates.Lookup): it never calls an FX provider or touches the
// network, per ADR-0004's "the application is fully usable with no
// network."
type ConvertAmountQuery struct {
	// Amount is the amount to convert, already in its own currency.
	Amount money.Money
	// To is the ISO 4217 currency code to convert Amount into.
	To string
	// Policy selects which date is looked up -- see ConversionPolicy.
	// Required.
	Policy ConversionPolicy
	// TransactionDate is the transaction's own booked date. Required (and
	// resolved via normalize.DateOf, so "today"/"yesterday"/ISO/locale
	// formats all work) when Policy is PolicyTransactionDate; ignored
	// otherwise.
	TransactionDate string
	// PinnedDate is the caller-chosen date. Required (resolved the same
	// way as TransactionDate) when Policy is PolicyPinned; ignored
	// otherwise.
	PinnedDate string
}

// ConvertedAmount is one converted figure with full ADR-0004 provenance:
// "there is no such thing as an unlabelled converted amount in this
// system." Every field below is part of that provenance except Amount
// itself.
type ConvertedAmount struct {
	// Amount is the converted figure, in the query's To currency.
	Amount money.Money
	// ConvertedFrom is the original amount, in its own currency -- the
	// query's Amount, echoed back so a caller holding only a
	// ConvertedAmount can still show both sides.
	ConvertedFrom money.Money
	// Rate is the exchange rate actually used: Base() equals
	// ConvertedFrom's currency, Quote() equals Amount's currency.
	Rate fx.Rate
	// RateDate is the calendar date Rate was recorded for -- which may be
	// earlier than the date the policy asked for, when Stale is true.
	RateDate domain.Date
	// RateSource names the provider Rate came from (e.g. "ecb"), or "" for
	// an identity conversion that never looked one up (ConvertedFrom's
	// currency already equals Amount's currency).
	RateSource string
	// Stale reports whether Rate was recorded for RateDate exactly (false)
	// or is a nearest-earlier fallback within the staleness window (true).
	Stale bool
	// Policy echoes the policy this conversion was computed under -- never
	// mixed with another policy within one aggregate (ADR-0004).
	Policy ConversionPolicy
}

// ConvertAmount implements issue #134's ADR-0004-compliant conversion
// query. Same-currency conversion is recognised up front and answered with
// an identity rate, without ever calling FxRates.Lookup -- there is
// nothing to look up, and no network dependency to introduce for the most
// common case (a user whose accounts are all in one currency).
//
// Missing/stale-rate handling is entirely FxRates.Lookup's
// (fx.SelectRate's) exact-match -> nearest-earlier-within-window ->
// ErrNoRateWithinWindow rule; ConvertAmount does not add another layer on
// top of it, and propagates whatever *errs.Error Lookup returns (NotFound
// for no rate within the window, InvalidInput for a bad window) as-is
// rather than re-wrapping it into something less specific.
func (s *Service) ConvertAmount(ctx context.Context, q ConvertAmountQuery) (ConvertedAmount, error) {
	if _, ok := money.LookupCurrency(q.To); !ok {
		return ConvertedAmount{}, errs.New(errs.InvalidInput).
			Explain("%q is not a known currency.", q.To).
			Field("to")
	}

	wantDate, err := s.resolveConversionDate(q)
	if err != nil {
		return ConvertedAmount{}, err
	}

	from := q.Amount.Currency()
	if from == q.To {
		rate, err := fx.IdentityRate(q.To)
		if err != nil {
			return ConvertedAmount{}, errs.New(errs.Internal).Wrap(err)
		}
		return ConvertedAmount{
			Amount:        q.Amount,
			ConvertedFrom: q.Amount,
			Rate:          rate,
			RateDate:      wantDate,
			RateSource:    "",
			Stale:         false,
			Policy:        q.Policy,
		}, nil
	}

	sel, err := s.FxRates.Lookup(ctx, from, q.To, wantDate, fx.DefaultStalenessWindowDays)
	if err != nil {
		return ConvertedAmount{}, err
	}

	converted, err := fx.Convert(q.Amount, q.To, sel.Rate)
	if err != nil {
		return ConvertedAmount{}, errs.New(errs.Internal).Wrap(err)
	}

	return ConvertedAmount{
		Amount:        converted,
		ConvertedFrom: q.Amount,
		Rate:          sel.Rate,
		RateDate:      sel.Date,
		RateSource:    sel.Source,
		Stale:         sel.Stale,
		Policy:        q.Policy,
	}, nil
}

// resolveConversionDate picks the date to pass as FxRates.Lookup's `want`
// argument, per q.Policy -- the "which date is passed... depends on the
// policy, not on 'today'" rule issue #134 exists to enforce. PolicyCurrent
// is the only policy that resolves "today", and it does so through
// normalize.DateOf (which itself only ever reads the service's clock, per
// ADR-0005 -- never time.Now() directly).
func (s *Service) resolveConversionDate(q ConvertAmountQuery) (domain.Date, error) {
	switch q.Policy {
	case PolicyTransactionDate:
		if strings.TrimSpace(q.TransactionDate) == "" {
			return domain.Date{}, errs.New(errs.InvalidInput).
				Explain("A transaction date is required to convert at the transaction_date policy.").
				Field("transaction_date")
		}
		return normalize.DateOf(q.TransactionDate, s.Clock, s.Config.UserTimezone)
	case PolicyCurrent:
		return normalize.DateOf("", s.Clock, s.Config.UserTimezone)
	case PolicyPinned:
		if strings.TrimSpace(q.PinnedDate) == "" {
			return domain.Date{}, errs.New(errs.InvalidInput).
				Explain("A pinned date is required to convert at the pinned policy.").
				Field("pinned_date")
		}
		return normalize.DateOf(q.PinnedDate, s.Clock, s.Config.UserTimezone)
	default:
		return domain.Date{}, errs.New(errs.InvalidInput).
			Explain("%q isn't a recognized conversion policy.", q.Policy).
			Field("policy").
			With("valid_policies", []string{string(PolicyTransactionDate), string(PolicyCurrent), string(PolicyPinned)})
	}
}
