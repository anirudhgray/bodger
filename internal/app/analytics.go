package app

import (
	"context"
	"math"
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

// Granularity selects how CashFlow/Trends bucket/compare periods (issue
// #194). Empty ("", the zero value every existing caller/test sends)
// means GranularityMonth — the fixed calendar-month behavior these two
// methods had before this type existed is unchanged by default.
// CategoryBreakdown/SavingsRate don't take a Granularity: they already
// operate over an arbitrary Filter date range with no periods to bucket
// or compare (issue #194's own scope note).
type Granularity string

const (
	GranularityWeek   Granularity = "week"
	GranularityMonth  Granularity = "month"
	GranularityYear   Granularity = "year"
	GranularityCustom Granularity = "custom"
)

// validateGranularity defaults "" to GranularityMonth (mirroring how
// validateAnalyticsOptions defaults nothing but rejects anything not
// recognized) and rejects anything that isn't one of the four values.
func validateGranularity(g Granularity) (Granularity, error) {
	if g == "" {
		g = GranularityMonth
	}
	switch g {
	case GranularityWeek, GranularityMonth, GranularityYear, GranularityCustom:
		return g, nil
	default:
		return "", errs.New(errs.InvalidInput).
			Explain("%q isn't a recognized granularity.", g).
			Field("granularity").
			With("valid_granularities", []string{string(GranularityWeek), string(GranularityMonth), string(GranularityYear), string(GranularityCustom)})
	}
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
	// Description is the owning transaction's own description (issue
	// #196's TopTransactions is the first caller to need it) — cheap to
	// carry since the Transaction is already in hand when this is built;
	// every other existing caller (CategoryBreakdown, CashFlow, Trends,
	// SavingsRate) simply doesn't read it.
	Description string
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
					Description:   txn.Description(),
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

	rows, err := categoryBreakdownRows(contributions, topLevel, opts.ReportingCurrency)
	if err != nil {
		return CategoryBreakdownResult{}, err
	}

	return CategoryBreakdownResult{Options: opts, Rows: rows, Unconverted: unconverted}, nil
}

// categoryBreakdownRows buckets contributions by top-level category (via
// topLevel, see topLevelCategoryIndex) into one CategoryBreakdownRow per
// bucket, converted into reportingCurrency, sorted by category name
// ascending with the uncategorized bucket last. Extracted from
// CategoryBreakdown itself (issue #196) so CategoryTrends can compute the
// same per-category rows for its own current/previous periods without
// duplicating this bucketing logic.
func categoryBreakdownRows(contributions []convertedContribution, topLevel map[string]ledger.Category, reportingCurrency string) ([]CategoryBreakdownRow, error) {
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
		spending, err := money.NewMoney(b.spend, reportingCurrency)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		income, err := money.NewMoney(b.income, reportingCurrency)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		net, err := money.NewMoney(b.income-b.spend, reportingCurrency)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
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

	return rows, nil
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

// CashFlowQuery groups inflow vs. outflow by period over Filter's
// matching transactions. Granularity selects the bucket size — see
// Granularity's own doc comment; "" defaults to GranularityMonth,
// today's original calendar-month bucketing.
type CashFlowQuery struct {
	ActorID     string
	Filter      TransactionFilterInput
	Options     AnalyticsOptions
	Granularity Granularity
}

// CashFlowPoint is one period's totals, converted into the query's
// reporting currency. From/To are that period's inclusive bounds —
// granularity-agnostic, since a week/year/custom bucket doesn't fit a
// single (year, month) pair the way a monthly one does. Outflow is
// reported as a positive magnitude (not negative) so a caller can plot
// inflow and outflow as two comparable bars/lines without negating
// anything itself.
type CashFlowPoint struct {
	From, To domain.Date
	Inflow   money.Money
	Outflow  money.Money
	Net      money.Money
}

// CashFlowResult is CashFlow's result: one point per period, sorted
// ascending. When Filter specifies both a DateFrom and a DateTo, every
// period in that closed range appears, including ones with no matching
// postings (Inflow/Outflow/Net all zero) — a continuous line chart needs
// the gaps, not just the periods that happened to have data. When either
// bound is left open, only periods that actually have a matching posting
// are reported, since there's no bound to zero-fill from. Under
// GranularityCustom, the entire Filter.DateFrom..DateTo range is exactly
// one bucket (both bounds are required in that case).
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
	granularity, err := validateGranularity(q.Granularity)
	if err != nil {
		return CashFlowResult{}, err
	}
	filter, err := s.resolveTransactionFilter(ctx, q.ActorID, q.Filter)
	if err != nil {
		return CashFlowResult{}, err
	}
	if granularity == GranularityCustom && (filter.FromDate == nil || filter.ToDate == nil) {
		return CashFlowResult{}, errs.New(errs.InvalidInput).
			Explain("A custom granularity needs both \"from\" and \"to\" to define its single bucket.").
			Field("granularity")
	}

	contributions, unconverted, err := s.convertPostings(ctx, q.ActorID, filter, opts)
	if err != nil {
		return CashFlowResult{}, err
	}

	type bucket struct {
		inflow, outflow int64
	}
	buckets := make(map[periodKey]*bucket)

	// Under GranularityCustom, every contribution falls into the one
	// bucket spanning the whole filter range — there's no per-posting
	// period to compute.
	var customKey periodKey
	if granularity == GranularityCustom {
		customKey = periodKey{From: *filter.FromDate, To: *filter.ToDate}
	}

	for _, c := range contributions {
		var key periodKey
		if granularity == GranularityCustom {
			key = customKey
		} else {
			key, err = periodContaining(c.BookedDate, granularity)
			if err != nil {
				return CashFlowResult{}, errs.New(errs.Internal).Wrap(err)
			}
		}
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

	var keys []periodKey
	switch {
	case granularity == GranularityCustom:
		keys = []periodKey{customKey}
		if _, ok := buckets[customKey]; !ok {
			buckets[customKey] = &bucket{}
		}
	case filter.FromDate != nil && filter.ToDate != nil:
		keys = periodKeysInRange(*filter.FromDate, *filter.ToDate, granularity)
		for _, k := range keys {
			if _, ok := buckets[k]; !ok {
				buckets[k] = &bucket{}
			}
		}
	default:
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
		points = append(points, CashFlowPoint{From: k.From, To: k.To, Inflow: inflow, Outflow: outflow, Net: net})
	}

	return CashFlowResult{Options: opts, Points: points, Unconverted: unconverted}, nil
}

// periodKey identifies one CashFlow bucket by its inclusive [From, To]
// span — granularity-agnostic, so week/month/year/custom buckets all
// share the same comparison/sort machinery rather than each granularity
// needing its own parallel key type.
type periodKey struct {
	From, To domain.Date
}

func (k periodKey) before(other periodKey) bool {
	return k.From.Before(other.From)
}

// periodContaining returns the periodKey of the period of the given
// granularity that date falls within. GranularityCustom has no fixed
// recurring period of its own — CashFlow handles that case separately
// (the whole filter range is one bucket) and never calls this with
// GranularityCustom.
func periodContaining(date domain.Date, g Granularity) (periodKey, error) {
	switch g {
	case GranularityWeek:
		from, to, err := weekRange(date)
		if err != nil {
			return periodKey{}, err
		}
		return periodKey{From: from, To: to}, nil
	case GranularityYear:
		from, to, err := yearRange(date.Year())
		if err != nil {
			return periodKey{}, err
		}
		return periodKey{From: from, To: to}, nil
	default: // GranularityMonth
		from, to, err := monthRange(date.Year(), date.Month())
		if err != nil {
			return periodKey{}, err
		}
		return periodKey{From: from, To: to}, nil
	}
}

// periodKeysInRange returns every period of granularity g overlapping
// [from, to], ascending, each clamped to nothing (a period always keeps
// its own full natural bounds, even the first/last one which may extend
// slightly outside [from, to]) — the same "zero-fill every period in the
// range" contract monthKeysInRange established, generalized to
// week/month/year. GranularityCustom is never passed here — CashFlow
// treats it as a single bucket built directly from the filter's own
// bounds.
func periodKeysInRange(from, to domain.Date, g Granularity) []periodKey {
	var keys []periodKey
	cur := from
	for {
		key, err := periodContaining(cur, g)
		if err != nil {
			break
		}
		keys = append(keys, key)
		if !key.To.Before(to) {
			break
		}
		cur = nextPeriodStart(key, g)
		if len(keys) > 12*200 {
			// A 200-year range is never a real query; this bound only
			// exists so a caller mistake can't spin this loop forever.
			break
		}
	}
	return keys
}

// nextPeriodStart returns the first date of the period immediately
// after key, for the given granularity.
func nextPeriodStart(key periodKey, g Granularity) domain.Date {
	switch g {
	case GranularityWeek:
		return addWeeks(key.From, 1)
	case GranularityYear:
		from, _, err := yearRange(key.From.Year() + 1)
		if err != nil {
			return key.To // unreachable: yearRange never fails on a real year
		}
		return from
	default: // GranularityMonth
		y, m := addMonths(key.From.Year(), key.From.Month(), 1)
		from, _, err := monthRange(y, m)
		if err != nil {
			return key.To // unreachable: monthRange never fails on a real (y, m)
		}
		return from
	}
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

// yearRange returns the first and last calendar Date of year.
func yearRange(year int) (domain.Date, domain.Date, error) {
	from, err := domain.NewDate(year, time.January, 1)
	if err != nil {
		return domain.Date{}, domain.Date{}, err
	}
	to, err := domain.NewDate(year, time.December, 31)
	if err != nil {
		return domain.Date{}, domain.Date{}, err
	}
	return from, to, nil
}

// weekRange returns the Monday-through-Sunday week containing date.
// Weeks start on Monday (ISO 8601's own convention) rather than
// Sunday — the unambiguous standard, avoiding the "does a week start
// Sunday or Monday" bikeshed a US-locale default would otherwise invite.
func weekRange(date domain.Date) (domain.Date, domain.Date, error) {
	t := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	// time.Weekday has Sunday = 0 ... Saturday = 6; ISO weekday has
	// Monday = 1 ... Sunday = 7. Converting lets "days since Monday" be
	// a single subtraction for every day of the week, Sunday included.
	isoWeekday := int(t.Weekday())
	if isoWeekday == 0 {
		isoWeekday = 7
	}
	monday := t.AddDate(0, 0, -(isoWeekday - 1))
	sunday := monday.AddDate(0, 0, 6)
	from, err := domain.NewDate(monday.Year(), monday.Month(), monday.Day())
	if err != nil {
		return domain.Date{}, domain.Date{}, err
	}
	to, err := domain.NewDate(sunday.Year(), sunday.Month(), sunday.Day())
	if err != nil {
		return domain.Date{}, domain.Date{}, err
	}
	return from, to, nil
}

// addWeeks returns the date delta whole weeks after from — delta may be
// negative to go backwards. Mirrors addMonths' round-trip-through-
// time.Date arithmetic pattern.
func addWeeks(from domain.Date, delta int) domain.Date {
	t := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	t = t.AddDate(0, 0, 7*delta)
	d, err := domain.NewDate(t.Year(), t.Month(), t.Day())
	if err != nil {
		// unreachable: time.Date's own round-trip normalization always
		// produces a valid calendar date.
		return from
	}
	return d
}

// ---- Trends ----

// TrendsQuery compares one period against the immediately preceding
// one, chosen by Granularity (see Granularity's own doc comment):
// GranularityMonth (the default, "") compares the current calendar
// month against the previous one; GranularityWeek the current Mon-Sun
// week against the previous one; GranularityYear the current calendar
// year against the previous one; GranularityCustom compares
// Filter.DateFrom..DateTo (both required) against the immediately
// preceding period of the same length. Except under GranularityCustom,
// Filter's own DateFrom/DateTo are ignored — Trends defines its own two
// periods — but every other dimension (account, category, currency,
// description, tags) still scopes both periods identically.
type TrendsQuery struct {
	ActorID     string
	Filter      TransactionFilterInput
	Options     AnalyticsOptions
	Granularity Granularity
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
	granularity, err := validateGranularity(q.Granularity)
	if err != nil {
		return TrendsResult{}, err
	}

	curFrom, curTo, prevFrom, prevTo, err := s.trendsComparisonPeriods(ctx, q.ActorID, q.Filter, granularity)
	if err != nil {
		return TrendsResult{}, err
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

// trendsComparisonPeriods resolves Trends' current/previous [from, to]
// bounds for granularity (already validated/defaulted by
// validateGranularity):
//
//   - GranularityMonth: the current calendar month vs. the previous one.
//   - GranularityWeek: the current Mon-Sun week vs. the previous one.
//   - GranularityYear: the current calendar year vs. the previous one.
//   - GranularityCustom: filterIn.DateFrom..DateTo (both required) vs.
//     the immediately preceding period of the same day count, ending
//     the day before DateFrom starts.
func (s *Service) trendsComparisonPeriods(ctx context.Context, actorID string, filterIn TransactionFilterInput, granularity Granularity) (curFrom, curTo, prevFrom, prevTo domain.Date, err error) {
	if granularity == GranularityCustom {
		filter, err := s.resolveTransactionFilter(ctx, actorID, filterIn)
		if err != nil {
			return domain.Date{}, domain.Date{}, domain.Date{}, domain.Date{}, err
		}
		if filter.FromDate == nil || filter.ToDate == nil {
			return domain.Date{}, domain.Date{}, domain.Date{}, domain.Date{}, errs.New(errs.InvalidInput).
				Explain("A custom granularity needs both \"from\" and \"to\" to define the current period.").
				Field("granularity")
		}
		curFrom, curTo = *filter.FromDate, *filter.ToDate
		// daysBetween is exclusive (0 when from == to); the period's own
		// inclusive day count is one more than that.
		days := daysBetween(curFrom, curTo) + 1
		prevTo = addDays(curFrom, -1)
		prevFrom = addDays(prevTo, -(days - 1))
		return curFrom, curTo, prevFrom, prevTo, nil
	}

	today, err := normalize.DateOf("", s.Clock, s.Config.UserTimezone)
	if err != nil {
		return domain.Date{}, domain.Date{}, domain.Date{}, domain.Date{}, err
	}

	switch granularity {
	case GranularityWeek:
		curFrom, curTo, err = weekRange(today)
		if err != nil {
			return domain.Date{}, domain.Date{}, domain.Date{}, domain.Date{}, errs.New(errs.Internal).Wrap(err)
		}
		prevFrom, prevTo = addWeeks(curFrom, -1), addWeeks(curTo, -1)
	case GranularityYear:
		curFrom, curTo, err = yearRange(today.Year())
		if err != nil {
			return domain.Date{}, domain.Date{}, domain.Date{}, domain.Date{}, errs.New(errs.Internal).Wrap(err)
		}
		prevFrom, prevTo, err = yearRange(today.Year() - 1)
		if err != nil {
			return domain.Date{}, domain.Date{}, domain.Date{}, domain.Date{}, errs.New(errs.Internal).Wrap(err)
		}
	default: // GranularityMonth
		curFrom, curTo, err = monthRange(today.Year(), today.Month())
		if err != nil {
			return domain.Date{}, domain.Date{}, domain.Date{}, domain.Date{}, errs.New(errs.Internal).Wrap(err)
		}
		prevYear, prevMonth := addMonths(today.Year(), today.Month(), -1)
		prevFrom, prevTo, err = monthRange(prevYear, prevMonth)
		if err != nil {
			return domain.Date{}, domain.Date{}, domain.Date{}, domain.Date{}, errs.New(errs.Internal).Wrap(err)
		}
	}
	return curFrom, curTo, prevFrom, prevTo, nil
}

// addDays returns the date delta whole days after from — delta may be
// negative to go backwards. Mirrors addWeeks/addMonths' round-trip-
// through-time.Date arithmetic pattern.
func addDays(from domain.Date, delta int) domain.Date {
	t := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	t = t.AddDate(0, 0, delta)
	d, err := domain.NewDate(t.Year(), t.Month(), t.Day())
	if err != nil {
		// unreachable: time.Date's own round-trip normalization always
		// produces a valid calendar date.
		return from
	}
	return d
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

// ---- TopTransactions ----

const (
	// defaultTopTransactionsLimit is TopTransactionsQuery.Limit's default
	// when the caller leaves it unset (<= 0) — a sensible "spot the
	// outliers at a glance" page size.
	defaultTopTransactionsLimit = 10
	// maxTopTransactionsLimit is the largest Limit TopTransactions
	// accepts: an unbounded "top N" defeats the point of the feature
	// (spotting outliers, not paginating every transaction) and risks a
	// very large response.
	maxTopTransactionsLimit = 100
)

// TopTransactionsQuery finds the largest N transactions (by absolute
// converted amount) matching Filter over Options' conversion.
type TopTransactionsQuery struct {
	ActorID string
	Filter  TransactionFilterInput
	Options AnalyticsOptions
	// Limit is the top-N count, distinct from TransactionFilterInput's
	// own (deliberately absent) pagination: every matching posting is
	// still fetched and converted first, and only *then* is the result
	// truncated to the Limit largest by magnitude. Defaults to
	// defaultTopTransactionsLimit when <= 0; rejected above
	// maxTopTransactionsLimit.
	Limit int
}

// TopTransactionsRow is one transaction's contribution, for spotting
// outlier expenses/income at a glance. Amount stays signed — negative for
// an outflow, positive for an inflow, matching convertedContribution's own
// convention — since the sign itself is informative ("this was my biggest
// expense" vs. "biggest inflow"); only the *sort* is by magnitude.
// Category is nil for an uncategorized posting, resolved the same way
// CategoryBreakdown resolves it (via topLevelCategoryIndex).
type TopTransactionsRow struct {
	TransactionID string
	Description   string
	BookedDate    domain.Date
	Category      *ledger.Category
	Amount        money.Money
}

// TopTransactionsResult is TopTransactions' result: the Limit largest
// matching postings by absolute amount, descending, ties broken by
// booked date descending and then transaction ID ascending — fully
// deterministic, so the same query returns the same order every time
// (needed for conformance comparison between the CLI and REST surfaces).
type TopTransactionsResult struct {
	Options     AnalyticsOptions
	Rows        []TopTransactionsRow
	Unconverted []UnconvertedPosting
}

// TopTransactions implements issue #196's top-transactions use case,
// reusing the same convertPostings core every other M5 analytics method
// uses rather than re-fetching or re-converting independently.
func (s *Service) TopTransactions(ctx context.Context, q TopTransactionsQuery) (TopTransactionsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return TopTransactionsResult{}, err
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultTopTransactionsLimit
	}
	if limit > maxTopTransactionsLimit {
		return TopTransactionsResult{}, errs.New(errs.InvalidInput).
			Explain("A limit of %d is too large; the maximum is %d.", q.Limit, maxTopTransactionsLimit).
			Field("limit")
	}

	opts, err := s.resolveAnalyticsOptions(ctx, q.ActorID, q.Options)
	if err != nil {
		return TopTransactionsResult{}, err
	}
	filter, err := s.resolveTransactionFilter(ctx, q.ActorID, q.Filter)
	if err != nil {
		return TopTransactionsResult{}, err
	}

	contributions, unconverted, err := s.convertPostings(ctx, q.ActorID, filter, opts)
	if err != nil {
		return TopTransactionsResult{}, err
	}

	categories, err := s.Categories.List(ctx, q.ActorID)
	if err != nil {
		return TopTransactionsResult{}, err
	}
	topLevel := topLevelCategoryIndex(categories)

	sort.Slice(contributions, func(i, j int) bool {
		ai, aj := contributions[i].Amount.Abs().AmountMinor(), contributions[j].Amount.Abs().AmountMinor()
		if ai != aj {
			return ai > aj
		}
		if !contributions[i].BookedDate.Equal(contributions[j].BookedDate) {
			return contributions[j].BookedDate.Before(contributions[i].BookedDate)
		}
		return contributions[i].TransactionID < contributions[j].TransactionID
	})

	if len(contributions) > limit {
		contributions = contributions[:limit]
	}

	rows := make([]TopTransactionsRow, 0, len(contributions))
	for _, c := range contributions {
		var cat *ledger.Category
		if c.CategoryID != "" {
			if top, ok := topLevel[c.CategoryID]; ok {
				t := top
				cat = &t
			}
		}
		rows = append(rows, TopTransactionsRow{
			TransactionID: c.TransactionID,
			Description:   c.Description,
			BookedDate:    c.BookedDate,
			Category:      cat,
			Amount:        c.Amount,
		})
	}

	return TopTransactionsResult{Options: opts, Rows: rows, Unconverted: unconverted}, nil
}

// ---- AverageTransactionSize ----

// AverageTransactionSizeQuery computes the mean transaction amount,
// overall and per top-level category, over Filter's matching
// transactions.
type AverageTransactionSizeQuery struct {
	ActorID string
	Filter  TransactionFilterInput
	Options AnalyticsOptions
}

// AverageTransactionSizeRow is one bucket's count and mean magnitude.
// Average is the mean of |Amount| — a magnitude, not a signed net: a
// signed mean over an expense-only category is just its total/count with
// the sign baked back in, which answers "what's my average net" rather
// than the more useful "how big are my transactions", so this always
// averages absolute values.
type AverageTransactionSizeRow struct {
	Category *ledger.Category
	Count    int
	Average  money.Money
}

// AverageTransactionSizeResult is AverageTransactionSize's result.
// Overall's own Category is always nil, but — unlike ByCategory's nil
// meaning "uncategorized" — here it simply has no category dimension at
// all: Overall represents the whole query, not one bucket among many, so
// it's its own field rather than a row that would collide in meaning with
// an uncategorized ByCategory row.
type AverageTransactionSizeResult struct {
	Options     AnalyticsOptions
	Overall     AverageTransactionSizeRow
	ByCategory  []AverageTransactionSizeRow
	Unconverted []UnconvertedPosting
}

// AverageTransactionSize implements issue #196's average-transaction-size
// use case, reusing convertPostings and topLevelCategoryIndex the same
// way CategoryBreakdown does.
func (s *Service) AverageTransactionSize(ctx context.Context, q AverageTransactionSizeQuery) (AverageTransactionSizeResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return AverageTransactionSizeResult{}, err
	}
	opts, err := s.resolveAnalyticsOptions(ctx, q.ActorID, q.Options)
	if err != nil {
		return AverageTransactionSizeResult{}, err
	}
	filter, err := s.resolveTransactionFilter(ctx, q.ActorID, q.Filter)
	if err != nil {
		return AverageTransactionSizeResult{}, err
	}

	contributions, unconverted, err := s.convertPostings(ctx, q.ActorID, filter, opts)
	if err != nil {
		return AverageTransactionSizeResult{}, err
	}

	categories, err := s.Categories.List(ctx, q.ActorID)
	if err != nil {
		return AverageTransactionSizeResult{}, err
	}
	topLevel := topLevelCategoryIndex(categories)

	type bucket struct {
		category *ledger.Category
		count    int
		sum      int64
	}
	buckets := make(map[string]*bucket)
	var overallCount int
	var overallSum int64

	for _, c := range contributions {
		magnitude := c.Amount.Abs().AmountMinor()
		overallCount++
		overallSum += magnitude

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
		b.count++
		b.sum += magnitude
	}

	overallAvg, err := money.NewMoney(roundedAverageMinor(overallSum, overallCount), opts.ReportingCurrency)
	if err != nil {
		return AverageTransactionSizeResult{}, errs.New(errs.Internal).Wrap(err)
	}
	overall := AverageTransactionSizeRow{Count: overallCount, Average: overallAvg}

	rows := make([]AverageTransactionSizeRow, 0, len(buckets))
	for _, b := range buckets {
		avg, err := money.NewMoney(roundedAverageMinor(b.sum, b.count), opts.ReportingCurrency)
		if err != nil {
			return AverageTransactionSizeResult{}, errs.New(errs.Internal).Wrap(err)
		}
		rows = append(rows, AverageTransactionSizeRow{Category: b.category, Count: b.count, Average: avg})
	}
	sort.Slice(rows, func(i, j int) bool {
		ci, cj := rows[i].Category, rows[j].Category
		if ci == nil {
			return false
		}
		if cj == nil {
			return true
		}
		return ci.Name() < cj.Name()
	})

	return AverageTransactionSizeResult{Options: opts, Overall: overall, ByCategory: rows, Unconverted: unconverted}, nil
}

// roundedAverageMinor rounds sumMinor/count to the nearest whole minor
// unit, half away from zero — math.Round already rounds this way for
// both positive and negative inputs, so this never silently truncates
// (which would bias every average down). count == 0 (AverageTransactionSize's
// Overall when literally nothing matched) returns 0 rather than dividing
// by zero.
func roundedAverageMinor(sumMinor int64, count int) int64 {
	if count == 0 {
		return 0
	}
	return int64(math.Round(float64(sumMinor) / float64(count)))
}

// ---- CategoryTrends ----

// CategoryTrendsQuery compares each top-level category's spending and
// income between the current and immediately preceding period, chosen by
// Granularity — the same current/previous period resolution Trends uses
// (trendsComparisonPeriods), applied per category rather than only in
// aggregate.
type CategoryTrendsQuery struct {
	ActorID     string
	Filter      TransactionFilterInput
	Options     AnalyticsOptions
	Granularity Granularity
}

// CategoryTrendDelta is one category's current-vs-previous comparison.
// Current/Previous are each that period's own CategoryBreakdownRow for
// this one category — zeroed (not omitted) on whichever side had no
// matching postings at all, the same "report zero, don't drop the row"
// convention CategoryBreakdownRow itself follows.
// SpendingChangePct/IncomeChangePct are nil when the previous period's
// corresponding figure is zero (changePct's own "undefined, not zero"
// contract).
type CategoryTrendDelta struct {
	Category          *ledger.Category
	Current           CategoryBreakdownRow
	Previous          CategoryBreakdownRow
	SpendingChangePct *float64
	IncomeChangePct   *float64
}

// CategoryTrendsResult is CategoryTrends' result: the resolved current
// and previous period bounds (echoed back the same way TrendsResult
// echoes its own, so a caller can render "vs last month (Aug 1-31)" per
// category the same way the aggregate Trends card does), one row per
// top-level category present in either period, sorted by name ascending
// with the uncategorized bucket last.
type CategoryTrendsResult struct {
	Options                                          AnalyticsOptions
	CurrentFrom, CurrentTo, PreviousFrom, PreviousTo domain.Date
	Rows                                             []CategoryTrendDelta
	Unconverted                                      []UnconvertedPosting
}

// CategoryTrends implements issue #196's per-category trend-deltas use
// case, extending Trends' aggregate-only comparison to one row per
// top-level category — reusing trendsComparisonPeriods for the period
// bounds and categoryBreakdownRows for each period's own per-category
// totals, rather than recomputing either independently.
func (s *Service) CategoryTrends(ctx context.Context, q CategoryTrendsQuery) (CategoryTrendsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return CategoryTrendsResult{}, err
	}
	opts, err := s.resolveAnalyticsOptions(ctx, q.ActorID, q.Options)
	if err != nil {
		return CategoryTrendsResult{}, err
	}
	granularity, err := validateGranularity(q.Granularity)
	if err != nil {
		return CategoryTrendsResult{}, err
	}

	curFrom, curTo, prevFrom, prevTo, err := s.trendsComparisonPeriods(ctx, q.ActorID, q.Filter, granularity)
	if err != nil {
		return CategoryTrendsResult{}, err
	}

	categories, err := s.Categories.List(ctx, q.ActorID)
	if err != nil {
		return CategoryTrendsResult{}, err
	}
	topLevel := topLevelCategoryIndex(categories)

	curRows, unconvertedCur, err := s.categoryTrendPeriod(ctx, q.ActorID, q.Filter, curFrom, curTo, opts, topLevel)
	if err != nil {
		return CategoryTrendsResult{}, err
	}
	prevRows, unconvertedPrev, err := s.categoryTrendPeriod(ctx, q.ActorID, q.Filter, prevFrom, prevTo, opts, topLevel)
	if err != nil {
		return CategoryTrendsResult{}, err
	}

	type merged struct {
		category *ledger.Category
		current  *CategoryBreakdownRow
		previous *CategoryBreakdownRow
	}
	entries := make(map[string]*merged)
	var order []string
	for i := range curRows {
		key := categoryTrendKey(curRows[i].Category)
		entries[key] = &merged{category: curRows[i].Category, current: &curRows[i]}
		order = append(order, key)
	}
	for i := range prevRows {
		key := categoryTrendKey(prevRows[i].Category)
		if e, ok := entries[key]; ok {
			e.previous = &prevRows[i]
		} else {
			entries[key] = &merged{category: prevRows[i].Category, previous: &prevRows[i]}
			order = append(order, key)
		}
	}

	rows := make([]CategoryTrendDelta, 0, len(entries))
	for _, key := range order {
		e := entries[key]
		current, previous := e.current, e.previous
		if current == nil {
			zero, err := zeroCategoryBreakdownRow(e.category, opts.ReportingCurrency)
			if err != nil {
				return CategoryTrendsResult{}, err
			}
			current = &zero
		}
		if previous == nil {
			zero, err := zeroCategoryBreakdownRow(e.category, opts.ReportingCurrency)
			if err != nil {
				return CategoryTrendsResult{}, err
			}
			previous = &zero
		}
		rows = append(rows, CategoryTrendDelta{
			Category:          e.category,
			Current:           *current,
			Previous:          *previous,
			SpendingChangePct: changePct(previous.Spending, current.Spending),
			IncomeChangePct:   changePct(previous.Income, current.Income),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		ci, cj := rows[i].Category, rows[j].Category
		if ci == nil {
			return false
		}
		if cj == nil {
			return true
		}
		return ci.Name() < cj.Name()
	})

	return CategoryTrendsResult{
		Options:      opts,
		CurrentFrom:  curFrom,
		CurrentTo:    curTo,
		PreviousFrom: prevFrom,
		PreviousTo:   prevTo,
		Rows:         rows,
		Unconverted:  append(unconvertedCur, unconvertedPrev...),
	}, nil
}

// categoryTrendKey keys CategoryTrends' per-period merge by category ID,
// "" for the uncategorized bucket — mirrors categoryBreakdownRows' own
// bucket key.
func categoryTrendKey(cat *ledger.Category) string {
	if cat == nil {
		return ""
	}
	return cat.ID()
}

// zeroCategoryBreakdownRow builds a CategoryBreakdownRow with every money
// field zeroed, for a category present in only one of CategoryTrends' two
// periods — CategoryBreakdownRow's own "report zero, don't omit" rule,
// applied to the side that had no matching postings in that period at
// all rather than leaving that side of the comparison absent.
func zeroCategoryBreakdownRow(cat *ledger.Category, currency string) (CategoryBreakdownRow, error) {
	zero, err := money.NewMoney(0, currency)
	if err != nil {
		return CategoryBreakdownRow{}, errs.New(errs.Internal).Wrap(err)
	}
	return CategoryBreakdownRow{Category: cat, Spending: zero, Income: zero, Net: zero}, nil
}

// categoryTrendPeriod computes one period's per-category breakdown rows,
// scoped by every dimension of filterIn except its own date bounds —
// mirrors trendPeriod's own "Trends supplies from/to itself" pattern, one
// level more granular (per category, not just the period aggregate).
func (s *Service) categoryTrendPeriod(ctx context.Context, actorID string, filterIn TransactionFilterInput, from, to domain.Date, opts AnalyticsOptions, topLevel map[string]ledger.Category) ([]CategoryBreakdownRow, []UnconvertedPosting, error) {
	filterIn.DateFrom = from.String()
	filterIn.DateTo = to.String()
	filter, err := s.resolveTransactionFilter(ctx, actorID, filterIn)
	if err != nil {
		return nil, nil, err
	}
	contributions, unconverted, err := s.convertPostings(ctx, actorID, filter, opts)
	if err != nil {
		return nil, nil, err
	}
	rows, err := categoryBreakdownRows(contributions, topLevel, opts.ReportingCurrency)
	if err != nil {
		return nil, nil, err
	}
	return rows, unconverted, nil
}
