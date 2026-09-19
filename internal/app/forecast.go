package app

import (
	"context"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// ForecastQuery asks for projected activity from pending ScheduledOccurrence
// rows over [FromDate, ToDate] — issue #280's forecast, kept as a wholly
// separate query from CashFlow/Trends rather than an optional field on
// either: data-model.md §11 requires projected money to be "always visually
// and structurally distinguished from actuals," and a separate method+result
// type makes that true at the type level (a caller has to deliberately call
// Forecast to get projected data at all) rather than relying on a sub-field
// a consumer could silently ignore or merge.
//
// FromDate/ToDate are both required, unlike CashFlow's open-ended Filter
// bounds: a forecast has no meaningful "everything" default, and ADR-0014's
// own 12-month generation horizon is the natural ceiling anyway — nothing
// pending exists past it.
type ForecastQuery struct {
	ActorID     string
	FromDate    string
	ToDate      string
	Options     AnalyticsOptions
	Granularity Granularity
}

// ForecastPoint is one period's projected totals, converted into the
// query's reporting currency. Field names are deliberately Projected* —
// not the bare Inflow/Outflow/Net CashFlowPoint uses — as an extra
// type-level guard against a future surface accidentally treating a
// ForecastPoint as a CashFlowPoint by careless field-name copy-paste, which
// Go's structural typing wouldn't otherwise catch.
type ForecastPoint struct {
	From, To         domain.Date
	ProjectedInflow  money.Money
	ProjectedOutflow money.Money
	ProjectedNet     money.Money
}

// UnconvertedOccurrence names one pending occurrence Forecast could not
// convert — no rate was available for its rule's account currency within
// the staleness window. Reported here, excluded from every total, the same
// treatment UnconvertedPosting gives an unconvertible posting (ADR-0004).
type UnconvertedOccurrence struct {
	OccurrenceID string
	Amount       money.Money
	Reason       string
}

// ForecastResult is Forecast's result: one point per period, sorted
// ascending, zero-filled across the entire [FromDate, ToDate] range (both
// bounds are always set, unlike CashFlow's open-ended filter) so a
// continuous line chart has every period to plot, not just the ones that
// happen to have a pending occurrence.
type ForecastResult struct {
	Options     AnalyticsOptions
	Points      []ForecastPoint
	Unconverted []UnconvertedOccurrence
}

// Forecast implements issue #280's projected-activity query. It reads only
// pending ScheduledOccurrence rows — materialised and skipped occurrences
// are resolved history, not a forecast — and never touches a balance,
// budget actual, or any other actuals-only query (data-model.md §11's
// "occurrences never contribute to a balance, report, or budget actual").
func (s *Service) Forecast(ctx context.Context, q ForecastQuery) (ForecastResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ForecastResult{}, err
	}
	opts, err := s.resolveAnalyticsOptions(ctx, q.ActorID, q.Options)
	if err != nil {
		return ForecastResult{}, err
	}
	granularity, err := validateGranularity(q.Granularity)
	if err != nil {
		return ForecastResult{}, err
	}
	from, err := normalize.DateOf(q.FromDate, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return ForecastResult{}, attachField(err, "from_date")
	}
	to, err := normalize.DateOf(q.ToDate, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return ForecastResult{}, attachField(err, "to_date")
	}
	if to.Before(from) {
		return ForecastResult{}, errs.New(errs.InvalidInput).
			Explain("\"to\" must not be before \"from\".").
			Field("to_date")
	}

	occurrences, err := s.ScheduledOccurrences.List(ctx, q.ActorID, ports.ScheduledOccurrenceFilter{
		Status:   recurring.OccurrenceStatusPending,
		FromDate: &from,
		ToDate:   &to,
	})
	if err != nil {
		return ForecastResult{}, err
	}

	rules, err := s.RecurringRules.List(ctx, q.ActorID)
	if err != nil {
		return ForecastResult{}, err
	}
	ruleByID := make(map[string]recurring.RecurringRule, len(rules))
	for _, r := range rules {
		ruleByID[r.ID()] = r
	}

	accountByID := make(map[string]ledger.Account)
	categoryByID := make(map[string]ledger.Category)

	type bucket struct {
		inflow, outflow int64
	}
	buckets := make(map[periodKey]*bucket)
	var unconverted []UnconvertedOccurrence

	for _, occ := range occurrences {
		rule, ok := ruleByID[occ.RuleID()]
		if !ok {
			// Unreachable in practice: every occurrence's rule_id is a
			// foreign key into recurring_rules, and both reads are scoped
			// to the same actor — kept as a defensive skip rather than a
			// panic, mirroring how convertPostings treats a per-row gap.
			continue
		}

		account, ok := accountByID[rule.AccountID()]
		if !ok {
			account, err = s.resolveOwnedAccount(ctx, q.ActorID, rule.AccountID())
			if err != nil {
				return ForecastResult{}, err
			}
			accountByID[rule.AccountID()] = account
		}
		category, ok := categoryByID[rule.CategoryID()]
		if !ok {
			category, err = s.resolveOwnedCategory(ctx, q.ActorID, rule.CategoryID())
			if err != nil {
				return ForecastResult{}, err
			}
			categoryByID[rule.CategoryID()] = category
		}

		wantPositive := category.Kind() == ledger.CategoryKindIncome
		amountMinor := signedAmount(rule.AmountMinor(), wantPositive)
		raw, err := money.NewMoney(amountMinor, account.Currency())
		if err != nil {
			return ForecastResult{}, err
		}

		converted, err := s.ConvertAmount(ctx, ConvertAmountQuery{
			Amount:          raw,
			To:              opts.ReportingCurrency,
			Policy:          opts.Policy,
			TransactionDate: occ.OccurrenceDate().String(),
			PinnedDate:      opts.PinnedDate,
		})
		switch {
		case err == nil:
			key, kerr := periodContaining(occ.OccurrenceDate(), granularity)
			if kerr != nil {
				return ForecastResult{}, errs.New(errs.Internal).Wrap(kerr)
			}
			b, ok := buckets[key]
			if !ok {
				b = &bucket{}
				buckets[key] = b
			}
			if converted.Amount.IsNegative() {
				b.outflow += converted.Amount.Abs().AmountMinor()
			} else {
				b.inflow += converted.Amount.AmountMinor()
			}
		case isNotFoundErr(err):
			unconverted = append(unconverted, UnconvertedOccurrence{
				OccurrenceID: occ.ID(),
				Amount:       raw,
				Reason:       errSafeMessage(err),
			})
		default:
			return ForecastResult{}, err
		}
	}

	keys := periodKeysInRange(from, to, granularity)
	for _, k := range keys {
		if _, ok := buckets[k]; !ok {
			buckets[k] = &bucket{}
		}
	}

	points := make([]ForecastPoint, 0, len(keys))
	for _, k := range keys {
		b := buckets[k]
		inflow, err := money.NewMoney(b.inflow, opts.ReportingCurrency)
		if err != nil {
			return ForecastResult{}, errs.New(errs.Internal).Wrap(err)
		}
		outflow, err := money.NewMoney(b.outflow, opts.ReportingCurrency)
		if err != nil {
			return ForecastResult{}, errs.New(errs.Internal).Wrap(err)
		}
		net, err := money.NewMoney(b.inflow-b.outflow, opts.ReportingCurrency)
		if err != nil {
			return ForecastResult{}, errs.New(errs.Internal).Wrap(err)
		}
		points = append(points, ForecastPoint{
			From: k.From, To: k.To,
			ProjectedInflow: inflow, ProjectedOutflow: outflow, ProjectedNet: net,
		})
	}

	return ForecastResult{Options: opts, Points: points, Unconverted: unconverted}, nil
}
