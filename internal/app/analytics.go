package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// AnalyticsOptions carries the reporting currency and conversion policy
// every M5 analytics method converts through (ADR-0004, ADR-0009: "opts
// carries the reporting currency and the conversion policy... the result
// carries them back so the caller can render provenance").
//
// An analytics result aggregates many postings, each potentially in its
// own currency, each converted at its own rate — there is no single Rate
// to attach to a bucket total the way ConvertedAmount does for one
// amount. So the results below echo ReportingCurrency and Policy back
// (the provenance that does apply uniformly to the whole result) and
// separately report any posting a rate couldn't be found for, rather
// than silently dropping it from the total or picking a different
// policy for it — the same rule AccountBalancesResult.Unconverted
// already established.
type AnalyticsOptions struct {
	// ReportingCurrency is the ISO 4217 code every converted figure in
	// the result is expressed in. Optional: a blank value is resolved via
	// ADR-0004's ladder (actor's own preference, then instance default) by
	// resolveAnalyticsOptions, the same way FetchFxRates resolves its own
	// reporting currency — a caller (CLI/HTTP) should never have to
	// re-derive the actor's preference itself just to call an analytics
	// method.
	ReportingCurrency string
	// Policy selects which of ADR-0004's conversion policies converts
	// each posting — see ConversionPolicy. Required.
	Policy ConversionPolicy
	// PinnedDate is the date to convert at when Policy is PolicyPinned.
	// Required in that case; ignored otherwise.
	PinnedDate string
}

// UnconvertedPosting names one posting an analytics query's requested
// conversion could not cover — no rate was available for its currency
// within the staleness window. It is reported here, excluded from every
// total, rather than silently omitted or converted under a different
// policy (ADR-0004).
type UnconvertedPosting struct {
	TransactionID string
	Amount        money.Money
	Reason        string
}

// resolveAnalyticsOptions fills in opts.ReportingCurrency via ADR-0004's
// ladder (actor's own preference, then instance default) when the caller
// left it blank — mirroring resolveFetchReportingCurrency: an analytics
// query has no entry or account of its own, so only those two rungs
// apply — then validates the result exactly as before.
func (s *Service) resolveAnalyticsOptions(ctx context.Context, actorID string, opts AnalyticsOptions) (AnalyticsOptions, error) {
	if strings.TrimSpace(opts.ReportingCurrency) == "" {
		preference, err := s.resolveReportingCurrency(ctx, actorID)
		if err != nil {
			return AnalyticsOptions{}, err
		}
		currency, err := normalize.Currency("", "", preference, s.Config.DefaultCurrency)
		if err != nil {
			return AnalyticsOptions{}, err
		}
		opts.ReportingCurrency = currency
	}
	return validateAnalyticsOptions(opts)
}

// validateAnalyticsOptions checks opts.ReportingCurrency is a known
// currency and opts.Policy is one of ADR-0004's three policies (and
// carries a PinnedDate when it's PolicyPinned) — validated up front so a
// query against zero matching transactions still reports a bad
// currency/policy rather than silently succeeding with an empty result.
func validateAnalyticsOptions(opts AnalyticsOptions) (AnalyticsOptions, error) {
	currency := strings.TrimSpace(opts.ReportingCurrency)
	if _, ok := money.LookupCurrency(currency); !ok {
		return AnalyticsOptions{}, errs.New(errs.InvalidInput).
			Explain("%q is not a known currency.", opts.ReportingCurrency).
			Field("reporting_currency")
	}
	opts.ReportingCurrency = currency

	switch opts.Policy {
	case PolicyTransactionDate, PolicyCurrent:
	case PolicyPinned:
		if strings.TrimSpace(opts.PinnedDate) == "" {
			return AnalyticsOptions{}, errs.New(errs.InvalidInput).
				Explain("A pinned date is required to convert at the pinned policy.").
				Field("pinned_date")
		}
	default:
		return AnalyticsOptions{}, errs.New(errs.InvalidInput).
			Explain("%q isn't a recognized conversion policy.", opts.Policy).
			Field("policy").
			With("valid_policies", []string{string(PolicyTransactionDate), string(PolicyCurrent), string(PolicyPinned)})
	}
	return opts, nil
}

// convertedContribution is one non-transfer posting, converted into an
// AnalyticsOptions.ReportingCurrency, with the context every M5
// analytics method needs to bucket it its own way (by category, by
// month, or into one period's total).
type convertedContribution struct {
	TransactionID string
	BookedDate    domain.Date
	CategoryID    string // "" for an uncategorized outflow/inflow posting
	Amount        money.Money
}

// convertPostings fetches every non-deleted transaction matching filter
// (unpaginated — like AccountBalances, an analytics aggregate needs
// every matching posting, not a page of them) and converts each of its
// postings into opts.ReportingCurrency under opts.Policy, at that
// posting's own transaction's booked date (PolicyTransactionDate's
// per-row lookup — see ConvertAmountQuery's doc comment on why a
// multi-date aggregate calls ConvertAmount once per row rather than
// resolving one date and reusing it).
//
// Transfer transactions are skipped entirely: ADR-0003 excludes
// transfers from spending/income analytics by construction, and a
// transfer's two postings (one on each of two accounts) aren't a
// spending or income event to attribute to either account.
//
// Every M5 analytics method (CategoryBreakdown, CashFlow, Trends,
// SavingsRate) calls this once and buckets its result differently,
// rather than each re-fetching and re-converting independently — issue
// #187's "[Trends] must reuse [cash-flow/category-breakdown data] rather
// than recomputing independently" applies to all four methods, not just
// Trends.
func (s *Service) convertPostings(ctx context.Context, actorID string, filter ports.TransactionFilter, opts AnalyticsOptions) ([]convertedContribution, []UnconvertedPosting, error) {
	txns, err := s.Transactions.List(ctx, actorID, filter)
	if err != nil {
		return nil, nil, err
	}

	var contributions []convertedContribution
	var unconverted []UnconvertedPosting
	for _, txn := range txns {
		if txn.Kind() == ledger.TransactionKindTransfer {
			continue
		}
		for _, p := range txn.Postings() {
			converted, err := s.ConvertAmount(ctx, ConvertAmountQuery{
				Amount:          p.Amount(),
				To:              opts.ReportingCurrency,
				Policy:          opts.Policy,
				TransactionDate: txn.BookedDate().String(),
				PinnedDate:      opts.PinnedDate,
			})
			switch {
			case err == nil:
				categoryID, _ := p.CategoryID()
				contributions = append(contributions, convertedContribution{
					TransactionID: txn.ID(),
					BookedDate:    txn.BookedDate(),
					CategoryID:    categoryID,
					Amount:        converted.Amount,
				})
			case isNotFoundErr(err):
				// A missing rate for this one posting's currency is the
				// shortfall ADR-0004 says to report explicitly, not a
				// reason to fail every other posting's total too.
				unconverted = append(unconverted, UnconvertedPosting{
					TransactionID: txn.ID(),
					Amount:        p.Amount(),
					Reason:        errSafeMessage(err),
				})
			default:
				// Anything else (an unrecognised policy, a missing
				// required date) is the same for every posting in this
				// query, not a per-row data gap — fail the whole query.
				return nil, nil, err
			}
		}
	}
	return contributions, unconverted, nil
}

// ---- CategoryBreakdown ----

// CategoryBreakdownQuery groups spending and income by top-level
// category over Filter's matching transactions.
type CategoryBreakdownQuery struct {
	ActorID string
	Filter  TransactionFilterInput
	Options AnalyticsOptions
}

// CategoryBreakdownRow is one category's totals: outflow and inflow
// summed separately (an expense category occasionally receives an
// inflow posting too — a refund posted with WithRelatedTransaction
// reuses the original outflow's category, per Transaction's own doc
// comment), each converted into the query's reporting currency. Net is
// Income − Spending, the single number a savings-adjacent chart wants
// without recomputing it client-side (ADR-0009: "the web UI does no
// maths").
//
// Category is nil for the "uncategorized" bucket — postings whose
// CategoryID is empty. Posting.CategoryID is optional even for an
// outflow/inflow posting (NewPosting); a transfer posting never carries
// one at all, but transfer postings never reach this bucketing in the
// first place (convertPostings skips transfer transactions entirely),
// so an empty CategoryID here always means "genuinely uncategorized",
// never "this was a transfer leg".
type CategoryBreakdownRow struct {
	Category *ledger.Category
	Spending money.Money
	Income   money.Money
	Net      money.Money
}

// CategoryBreakdownResult is CategoryBreakdown's result: one row per
// top-level category (plus, when present, the uncategorized bucket)
// with at least one matching posting, sorted by category name ascending
// (uncategorized sorts last) for a deterministic, chart-ready order.
type CategoryBreakdownResult struct {
	Options     AnalyticsOptions
	Rows        []CategoryBreakdownRow
	Unconverted []UnconvertedPosting
}

// CategoryBreakdown implements issue #187's category-breakdown use
// case. Every matching posting rolls up into its *top-level* category
// bucket — ADR-0009's own illustrative example for why CategoryRefs
// includes the subtree by default ("'Food' means Groceries,
// Restaurants, and Delivery — that is what users mean") applies just as
// much to a breakdown chart: a bucket per leaf category would fragment
// a user's spending picture across however many subcategories they
// happen to have. A caller that wants one specific subtree's rows can
// still scope the whole query to it via Filter.CategoryRef, which
// already includes that subtree by construction (ports.TransactionFilter's
// own doc comment).
func (s *Service) CategoryBreakdown(ctx context.Context, q CategoryBreakdownQuery) (CategoryBreakdownResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return CategoryBreakdownResult{}, err
	}
	opts, err := s.resolveAnalyticsOptions(ctx, q.ActorID, q.Options)
	if err != nil {
		return CategoryBreakdownResult{}, err
	}
	filter, err := s.resolveTransactionFilter(ctx, q.ActorID, q.Filter)
	if err != nil {
		return CategoryBreakdownResult{}, err
	}

	contributions, unconverted, err := s.convertPostings(ctx, q.ActorID, filter, opts)
	if err != nil {
		return CategoryBreakdownResult{}, err
	}

	categories, err := s.Categories.List(ctx, q.ActorID)
	if err != nil {
		return CategoryBreakdownResult{}, err
	}
	topLevel := topLevelCategoryIndex(categories)

	type bucket struct {
		category      *ledger.Category
		spend, income int64
	}
	buckets := make(map[string]*bucket) // keyed by top-level category ID, or "" for uncategorized

	for _, c := range contributions {
		key := ""
		var cat *ledger.Category
		if c.CategoryID != "" {
			if top, ok := topLevel[c.CategoryID]; ok {
				t := top
				cat = &t
				key = top.ID()
			}
		}
		b, ok := buckets[key]
		if !ok {
			b = &bucket{category: cat}
			buckets[key] = b
		}
		if c.Amount.IsNegative() {
			b.spend += c.Amount.Abs().AmountMinor()
		} else {
			b.income += c.Amount.AmountMinor()
		}
	}

	rows := make([]CategoryBreakdownRow, 0, len(buckets))
	for _, b := range buckets {
		spending, err := money.NewMoney(b.spend, opts.ReportingCurrency)
		if err != nil {
			return CategoryBreakdownResult{}, errs.New(errs.Internal).Wrap(err)
		}
		income, err := money.NewMoney(b.income, opts.ReportingCurrency)
		if err != nil {
			return CategoryBreakdownResult{}, errs.New(errs.Internal).Wrap(err)
		}
		net, err := money.NewMoney(b.income-b.spend, opts.ReportingCurrency)
		if err != nil {
			return CategoryBreakdownResult{}, errs.New(errs.Internal).Wrap(err)
		}
		rows = append(rows, CategoryBreakdownRow{Category: b.category, Spending: spending, Income: income, Net: net})
	}

	sort.Slice(rows, func(i, j int) bool {
		ci, cj := rows[i].Category, rows[j].Category
		if ci == nil {
			return false // uncategorized always sorts last
		}
		if cj == nil {
			return true
		}
		return ci.Name() < cj.Name()
	})

	return CategoryBreakdownResult{Options: opts, Rows: rows, Unconverted: unconverted}, nil
}

// topLevelCategoryIndex maps every category ID (at any depth) to a copy
// of its top-level ancestor category — itself, if it's already
// top-level. It trusts the category graph is already acyclic, the same
// assumption buildCategoryTree (categories.go) makes, for the same
// reason: every write path that can produce a Category refuses to
// introduce a cycle.
func topLevelCategoryIndex(categories []ledger.Category) map[string]ledger.Category {
	byID := make(map[string]ledger.Category, len(categories))
	for _, c := range categories {
		byID[c.ID()] = c
	}

	index := make(map[string]ledger.Category, len(categories))
	for _, c := range categories {
		cur := c
		for {
			parentID, ok := cur.ParentID()
			if !ok {
				break
			}
			parent, ok := byID[parentID]
			if !ok {
				break
			}
			cur = parent
		}
		index[c.ID()] = cur
	}
	return index
}

// ---- CashFlow ----

// CashFlowQuery groups inflow vs. outflow by calendar month over
// Filter's matching transactions.
type CashFlowQuery struct {
	ActorID string
	Filter  TransactionFilterInput
	Options AnalyticsOptions
}

// CashFlowPoint is one calendar month's totals, converted into the
// query's reporting currency. Outflow is reported as a positive
// magnitude (not negative) so a caller can plot inflow and outflow as
// two comparable bars/lines without negating anything itself.
type CashFlowPoint struct {
	Year    int
	Month   time.Month
	Inflow  money.Money
	Outflow money.Money
	Net     money.Money
}

// CashFlowResult is CashFlow's result: one point per calendar month,
// sorted ascending. When Filter specifies both a DateFrom and a DateTo,
// every month in that closed range appears, including months with no
// matching postings (Inflow/Outflow/Net all zero) — a continuous line
// chart needs the gaps, not just the months that happened to have data.
// When either bound is left open, only months that actually have a
// matching posting are reported, since there's no bound to zero-fill
// from.
type CashFlowResult struct {
	Options     AnalyticsOptions
	Points      []CashFlowPoint
	Unconverted []UnconvertedPosting
}

// CashFlow implements issue #187's cash-flow use case.
func (s *Service) CashFlow(ctx context.Context, q CashFlowQuery) (CashFlowResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return CashFlowResult{}, err
	}
	opts, err := s.resolveAnalyticsOptions(ctx, q.ActorID, q.Options)
	if err != nil {
		return CashFlowResult{}, err
	}
	filter, err := s.resolveTransactionFilter(ctx, q.ActorID, q.Filter)
	if err != nil {
		return CashFlowResult{}, err
	}

	contributions, unconverted, err := s.convertPostings(ctx, q.ActorID, filter, opts)
	if err != nil {
		return CashFlowResult{}, err
	}

	type bucket struct {
		inflow, outflow int64
	}
	buckets := make(map[monthKey]*bucket)
	for _, c := range contributions {
		key := monthKey{Year: c.BookedDate.Year(), Month: c.BookedDate.Month()}
		b, ok := buckets[key]
		if !ok {
			b = &bucket{}
			buckets[key] = b
		}
		if c.Amount.IsNegative() {
			b.outflow += c.Amount.Abs().AmountMinor()
		} else {
			b.inflow += c.Amount.AmountMinor()
		}
	}

	var keys []monthKey
	if filter.FromDate != nil && filter.ToDate != nil {
		keys = monthKeysInRange(*filter.FromDate, *filter.ToDate)
		for _, k := range keys {
			if _, ok := buckets[k]; !ok {
				buckets[k] = &bucket{}
			}
		}
	} else {
		for k := range buckets {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].before(keys[j]) })

	points := make([]CashFlowPoint, 0, len(keys))
	for _, k := range keys {
		b := buckets[k]
		inflow, err := money.NewMoney(b.inflow, opts.ReportingCurrency)
		if err != nil {
			return CashFlowResult{}, errs.New(errs.Internal).Wrap(err)
		}
		outflow, err := money.NewMoney(b.outflow, opts.ReportingCurrency)
		if err != nil {
			return CashFlowResult{}, errs.New(errs.Internal).Wrap(err)
		}
		net, err := money.NewMoney(b.inflow-b.outflow, opts.ReportingCurrency)
		if err != nil {
			return CashFlowResult{}, errs.New(errs.Internal).Wrap(err)
		}
		points = append(points, CashFlowPoint{Year: k.Year, Month: k.Month, Inflow: inflow, Outflow: outflow, Net: net})
	}

	return CashFlowResult{Options: opts, Points: points, Unconverted: unconverted}, nil
}

// monthKey identifies one calendar month.
type monthKey struct {
	Year  int
	Month time.Month
}

func (k monthKey) before(other monthKey) bool {
	if k.Year != other.Year {
		return k.Year < other.Year
	}
	return k.Month < other.Month
}

// addMonths returns the (year, month) delta whole months after (year,
// month) — delta may be negative to go backwards. Pure integer
// arithmetic, no clock, safe to live alongside the analytics methods
// that use it.
func addMonths(year int, month time.Month, delta int) (int, time.Month) {
	total := int(month) - 1 + delta
	y := year + total/12
	m := total % 12
	if m < 0 {
		m += 12
		y--
	}
	return y, time.Month(m + 1)
}

// monthKeysInRange returns every calendar month from from through to,
// inclusive, ascending.
func monthKeysInRange(from, to domain.Date) []monthKey {
	var keys []monthKey
	y, m := from.Year(), from.Month()
	for {
		keys = append(keys, monthKey{Year: y, Month: m})
		if y == to.Year() && m == to.Month() {
			break
		}
		y, m = addMonths(y, m, 1)
		if len(keys) > 12*200 {
			// A 200-year range is never a real query; this bound only
			// exists so a caller mistake can't spin this loop forever.
			break
		}
	}
	return keys
}

// monthRange returns the first and last calendar Date of (year, month).
func monthRange(year int, month time.Month) (domain.Date, domain.Date, error) {
	from, err := domain.NewDate(year, month, 1)
	if err != nil {
		return domain.Date{}, domain.Date{}, err
	}
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
	to, err := domain.NewDate(lastDay.Year(), lastDay.Month(), lastDay.Day())
	if err != nil {
		return domain.Date{}, domain.Date{}, err
	}
	return from, to, nil
}

// ---- Trends ----

// TrendsQuery compares the current calendar month against the previous
// one. Filter's DateFrom/DateTo are ignored — Trends defines its own
// two periods — but every other dimension (account, category,
// currency, description, tags) still scopes both periods identically.
type TrendsQuery struct {
	ActorID string
	Filter  TransactionFilterInput
	Options AnalyticsOptions
}

// TrendPeriod is one period's totals in a Trends comparison.
type TrendPeriod struct {
	From, To domain.Date
	Inflow   money.Money
	Outflow  money.Money
	Net      money.Money
}

// TrendsResult is Trends' result: the current and previous calendar
// month's totals, plus each one's percentage change from the previous
// period. A change percentage is nil when the previous period's value
// is zero — division by zero has no meaningful percentage, and ADR-0009
// forbids the web UI from inventing one client-side.
type TrendsResult struct {
	Options          AnalyticsOptions
	Current          TrendPeriod
	Previous         TrendPeriod
	InflowChangePct  *float64
	OutflowChangePct *float64
	Unconverted      []UnconvertedPosting
}

// Trends implements issue #187's trends use case, reusing CashFlow's
// per-month aggregation (via the same convertPostings core) for both
// periods rather than recomputing anything independently.
func (s *Service) Trends(ctx context.Context, q TrendsQuery) (TrendsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return TrendsResult{}, err
	}
	opts, err := s.resolveAnalyticsOptions(ctx, q.ActorID, q.Options)
	if err != nil {
		return TrendsResult{}, err
	}

	today, err := normalize.DateOf("", s.Clock, s.Config.UserTimezone)
	if err != nil {
		return TrendsResult{}, err
	}
	curFrom, curTo, err := monthRange(today.Year(), today.Month())
	if err != nil {
		return TrendsResult{}, errs.New(errs.Internal).Wrap(err)
	}
	prevYear, prevMonth := addMonths(today.Year(), today.Month(), -1)
	prevFrom, prevTo, err := monthRange(prevYear, prevMonth)
	if err != nil {
		return TrendsResult{}, errs.New(errs.Internal).Wrap(err)
	}

	current, unconvertedCur, err := s.trendPeriod(ctx, q.ActorID, q.Filter, curFrom, curTo, opts)
	if err != nil {
		return TrendsResult{}, err
	}
	previous, unconvertedPrev, err := s.trendPeriod(ctx, q.ActorID, q.Filter, prevFrom, prevTo, opts)
	if err != nil {
		return TrendsResult{}, err
	}

	return TrendsResult{
		Options:          opts,
		Current:          current,
		Previous:         previous,
		InflowChangePct:  changePct(previous.Inflow, current.Inflow),
		OutflowChangePct: changePct(previous.Outflow, current.Outflow),
		Unconverted:      append(unconvertedCur, unconvertedPrev...),
	}, nil
}

// trendPeriod computes one TrendPeriod's totals over [from, to],
// scoped by every dimension of filterIn except its own date bounds
// (Trends supplies from/to itself).
func (s *Service) trendPeriod(ctx context.Context, actorID string, filterIn TransactionFilterInput, from, to domain.Date, opts AnalyticsOptions) (TrendPeriod, []UnconvertedPosting, error) {
	filterIn.DateFrom = from.String()
	filterIn.DateTo = to.String()
	filter, err := s.resolveTransactionFilter(ctx, actorID, filterIn)
	if err != nil {
		return TrendPeriod{}, nil, err
	}

	contributions, unconverted, err := s.convertPostings(ctx, actorID, filter, opts)
	if err != nil {
		return TrendPeriod{}, nil, err
	}

	var inflowMinor, outflowMinor int64
	for _, c := range contributions {
		if c.Amount.IsNegative() {
			outflowMinor += c.Amount.Abs().AmountMinor()
		} else {
			inflowMinor += c.Amount.AmountMinor()
		}
	}
	inflow, err := money.NewMoney(inflowMinor, opts.ReportingCurrency)
	if err != nil {
		return TrendPeriod{}, nil, errs.New(errs.Internal).Wrap(err)
	}
	outflow, err := money.NewMoney(outflowMinor, opts.ReportingCurrency)
	if err != nil {
		return TrendPeriod{}, nil, errs.New(errs.Internal).Wrap(err)
	}
	net, err := money.NewMoney(inflowMinor-outflowMinor, opts.ReportingCurrency)
	if err != nil {
		return TrendPeriod{}, nil, errs.New(errs.Internal).Wrap(err)
	}
	return TrendPeriod{From: from, To: to, Inflow: inflow, Outflow: outflow, Net: net}, unconverted, nil
}

// changePct returns (curr-prev)/prev as a percentage, or nil if prev is
// zero (an undefined percentage change, not zero).
func changePct(prev, curr money.Money) *float64 {
	if prev.IsZero() {
		return nil
	}
	pct := float64(curr.AmountMinor()-prev.AmountMinor()) / float64(prev.AmountMinor()) * 100
	return &pct
}

// ---- SavingsRate ----

// SavingsRateQuery computes (income − outflow) / income over Filter's
// matching transactions and date range.
type SavingsRateQuery struct {
	ActorID string
	Filter  TransactionFilterInput
	Options AnalyticsOptions
}

// SavingsRateResult is SavingsRate's result. Rate is nil when Income is
// zero — the ratio is undefined, not zero, and ADR-0009 forbids
// inventing a fallback client-side.
type SavingsRateResult struct {
	Options     AnalyticsOptions
	Income      money.Money
	Outflow     money.Money
	Net         money.Money
	Rate        *float64
	Unconverted []UnconvertedPosting
}

// SavingsRate implements issue #187's savings-rate use case, derived
// from the same per-posting conversion core CashFlow and Trends use
// (convertPostings), summed over Filter's own date range rather than a
// month Trends/CashFlow impose — a caller wanting "this month's savings
// rate" sets Filter.DateFrom/DateTo to that month itself.
func (s *Service) SavingsRate(ctx context.Context, q SavingsRateQuery) (SavingsRateResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return SavingsRateResult{}, err
	}
	opts, err := s.resolveAnalyticsOptions(ctx, q.ActorID, q.Options)
	if err != nil {
		return SavingsRateResult{}, err
	}
	filter, err := s.resolveTransactionFilter(ctx, q.ActorID, q.Filter)
	if err != nil {
		return SavingsRateResult{}, err
	}

	contributions, unconverted, err := s.convertPostings(ctx, q.ActorID, filter, opts)
	if err != nil {
		return SavingsRateResult{}, err
	}

	var inflowMinor, outflowMinor int64
	for _, c := range contributions {
		if c.Amount.IsNegative() {
			outflowMinor += c.Amount.Abs().AmountMinor()
		} else {
			inflowMinor += c.Amount.AmountMinor()
		}
	}
	income, err := money.NewMoney(inflowMinor, opts.ReportingCurrency)
	if err != nil {
		return SavingsRateResult{}, errs.New(errs.Internal).Wrap(err)
	}
	outflow, err := money.NewMoney(outflowMinor, opts.ReportingCurrency)
	if err != nil {
		return SavingsRateResult{}, errs.New(errs.Internal).Wrap(err)
	}
	net, err := money.NewMoney(inflowMinor-outflowMinor, opts.ReportingCurrency)
	if err != nil {
		return SavingsRateResult{}, errs.New(errs.Internal).Wrap(err)
	}

	var rate *float64
	if inflowMinor != 0 {
		r := float64(inflowMinor-outflowMinor) / float64(inflowMinor)
		rate = &r
	}

	return SavingsRateResult{
		Options:     opts,
		Income:      income,
		Outflow:     outflow,
		Net:         net,
		Rate:        rate,
		Unconverted: unconverted,
	}, nil
}
