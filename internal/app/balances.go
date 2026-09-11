package app

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// AccountBalancesQuery computes every account's balance as of AsOf
// (ADR-0002): opening balance plus every non-deleted posting on or before
// that date. AsOf resolves through normalize.DateOf, so an empty string
// means "today" in the actor's configured timezone.
//
// TargetCurrency, Policy, and PinnedDate are all optional together: leaving
// TargetCurrency empty keeps this query's original single-currency
// behaviour exactly (no conversion, no provenance, Unconverted always
// empty) -- existing callers are unaffected. Setting TargetCurrency
// converts every account's balance into it via ConvertAmount, under
// Policy, which must then be set too.
type AccountBalancesQuery struct {
	ActorID string
	AsOf    string

	// TargetCurrency, when non-empty, is the ISO 4217 currency code to
	// convert every account's balance into.
	TargetCurrency string
	// Policy selects which of ADR-0004's conversion policies converts each
	// balance -- see ConversionPolicy. Required when TargetCurrency is
	// set; ignored otherwise.
	//
	// PolicyTransactionDate converts each balance at the resolved AsOf
	// date's rate: a balance "as of" a date is exactly the reproducible,
	// historical figure that policy is for. PolicyCurrent converts every
	// balance at today's rate regardless of AsOf -- ADR-0004's own example
	// of what that policy is for ("current balances, net worth").
	// PolicyPinned converts at PinnedDate.
	Policy ConversionPolicy
	// PinnedDate is the date to convert at when Policy is PolicyPinned.
	// Required in that case; ignored otherwise.
	PinnedDate string
}

// AccountBalance pairs one Account with its computed balance as of the
// query's AsOf date, plus (when the query set a TargetCurrency) that
// balance's converted figure and provenance.
type AccountBalance struct {
	Account ledger.Account
	Balance money.Money
	// Converted holds Balance's converted figure and full ADR-0004
	// provenance when the query set a TargetCurrency and a rate was
	// available for this account's currency; nil when the query didn't
	// ask for a conversion, or when this account's balance couldn't be
	// converted (see AccountBalancesResult.Unconverted).
	Converted *ConvertedAmount
}

// UnconvertedBalance names an account whose balance the query's requested
// conversion could not produce -- no rate was available for its currency
// within the staleness window. ADR-0004's "mixed-policy aggregates are
// forbidden... if some transactions in a range have no available rate, the
// response reports the shortfall explicitly... rather than silently
// omitting them or falling back to a different policy for those rows": an
// unconvertible account is reported here, not dropped from Balances and
// not converted under some other policy.
type UnconvertedBalance struct {
	Account ledger.Account
	// Reason is the safe, human-readable explanation the failed
	// ConvertAmount call returned (e.g. "no INR/EUR rate available for
	// 2026-08-14 within 7 day(s).").
	Reason string
}

// AccountBalancesResult is AccountBalances' result: the resolved AsOf date
// (so a caller that passed "" or "today" can see what date it actually
// resolved to), every account's balance, in the same name order
// ports.AccountRepository.List returns, and (only when the query requested
// a conversion) any accounts that conversion couldn't cover.
type AccountBalancesResult struct {
	AsOf        domain.Date
	Balances    []AccountBalance
	Unconverted []UnconvertedBalance
}

// AccountBalances implements issue #6's AccountBalances use case. Nothing
// here is stored or cached — every balance is recomputed from postings on
// every call, per ADR-0002 ("no cached balances... only as a measured
// performance optimisation, and only behind a test asserting the cached
// value equals the recomputed value... until such a test exists, the
// cache does not"). That test doesn't exist, so neither does a cache.
//
// Every account's balance is computed from a single Transactions.List call
// (filtered only by ToDate, unpaginated - ports.TransactionFilter's
// Limit <= 0 means "no limit", deliberately, for exactly this caller) run
// once, rather than one List call per account: ledger.Balance already
// ignores postings on accounts other than the one it's given, so filtering
// per-account at the query layer would only add N-1 unnecessary round
// trips for N accounts.
func (s *Service) AccountBalances(ctx context.Context, q AccountBalancesQuery) (AccountBalancesResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return AccountBalancesResult{}, err
	}

	asOf, err := normalize.DateOf(q.AsOf, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return AccountBalancesResult{}, err
	}

	accounts, err := s.Accounts.List(ctx, q.ActorID)
	if err != nil {
		return AccountBalancesResult{}, err
	}

	txns, err := s.Transactions.List(ctx, q.ActorID, ports.TransactionFilter{ToDate: &asOf})
	if err != nil {
		return AccountBalancesResult{}, err
	}

	convert := q.TargetCurrency != ""

	balances := make([]AccountBalance, 0, len(accounts))
	var unconverted []UnconvertedBalance
	for _, account := range accounts {
		balance, err := ledger.Balance(account, asOf, txns)
		if err != nil {
			return AccountBalancesResult{}, errs.New(errs.Internal).Wrap(err)
		}

		ab := AccountBalance{Account: account, Balance: balance}
		if convert {
			converted, err := s.ConvertAmount(ctx, ConvertAmountQuery{
				Amount:          balance,
				To:              q.TargetCurrency,
				Policy:          q.Policy,
				TransactionDate: asOf.String(),
				PinnedDate:      q.PinnedDate,
			})
			switch {
			case err == nil:
				ab.Converted = &converted
			case isNotFoundErr(err):
				// A missing rate for this one account's currency is the
				// shortfall ADR-0004 says to report explicitly, not a
				// reason to fail every other account's balance too.
				unconverted = append(unconverted, UnconvertedBalance{Account: account, Reason: errSafeMessage(err)})
			default:
				// Anything else (an unrecognised policy, a missing
				// required date, an unknown target currency) is the same
				// for every account in this query, not a per-account data
				// gap -- fail the whole query rather than silently
				// omitting it.
				return AccountBalancesResult{}, err
			}
		}
		balances = append(balances, ab)
	}
	return AccountBalancesResult{AsOf: asOf, Balances: balances, Unconverted: unconverted}, nil
}

// ---- BalanceTotals ----

// BalanceTotalsQuery computes issue #195's totals overview: the overall net
// balance, a per-account-category breakdown, and a per-currency (raw)
// breakdown, as of AsOf. It builds entirely on AccountBalances -- same
// accounts, same transactions, same per-account conversion and
// shortfall-reporting -- rather than recomputing any of that
// independently.
type BalanceTotalsQuery struct {
	ActorID string
	AsOf    string

	// TargetCurrency optionally overrides the reporting currency Overall
	// and ByCategory are converted into. Left blank, it resolves through
	// ADR-0004's ladder -- ActorID's own reporting-currency preference,
	// falling through to the instance default -- the same two-step
	// resolution resolveFetchReportingCurrency (fetch_fx_rates.go) already
	// applies, mirrored here rather than duplicated as a shared helper:
	// the two callers differ in exactly one respect (this one accepts an
	// explicit override; that one never does), which isn't enough shared
	// behaviour to justify the indirection of factoring out two lines.
	TargetCurrency string
	// Policy selects which of ADR-0004's conversion policies converts
	// every account's balance into the reporting currency -- see
	// ConversionPolicy. Required: Overall and ByCategory are always
	// converted aggregates, like every M5 analytics result
	// (AnalyticsOptions.Policy), never left unconverted the way
	// AccountBalances' own per-account figures can be.
	Policy ConversionPolicy
	// PinnedDate is the date to convert at when Policy is PolicyPinned.
	// Required in that case; ignored otherwise.
	PinnedDate string
}

// AccountKindTotal is one ledger.AccountKind's balances, summed across
// every account of that kind and converted into the query's reporting
// currency.
type AccountKindTotal struct {
	Kind  ledger.AccountKind
	Total money.Money
}

// CurrencyTotal is every account in one currency, summed in that currency
// -- raw, unconverted, per issue #195's own "per currency (raw,
// unconverted)" requirement. There is no conversion to fail here, so an
// account never needs to appear in BalanceTotalsResult.Unconverted on this
// breakdown's account: every account contributes to its own currency's
// total regardless of whether Overall/ByCategory's conversion could cover
// it.
type CurrencyTotal struct {
	Currency string
	Total    money.Money
}

// BalanceTotalsResult is BalanceTotals' result.
type BalanceTotalsResult struct {
	AsOf domain.Date
	// Options echoes the reporting currency and policy Overall and
	// ByCategory were converted under -- the same provenance-by-echo M5's
	// AnalyticsOptions-carrying results already use for a multi-row
	// aggregate that has no single ConvertedAmount of its own to attach
	// per-value provenance to.
	Options AnalyticsOptions

	// Overall is the net balance across every account, converted into
	// Options.ReportingCurrency. Liability accounts (credit_card, loan)
	// already reduce it without any special-casing here:
	// data-model.md §4 and ledger.Balance's own contract store a
	// liability account's balance negative exactly when money is owed, so
	// a plain sum of every account's converted balance already nets
	// liabilities against assets -- there is no second sign flip to apply
	// on top of that.
	Overall money.Money

	// ByCategory has one row per ledger.AccountKind present among the
	// actor's accounts (the same accounts AccountBalances itself
	// includes -- archived accounts are not filtered out, matching that
	// method's own behaviour), each summed and converted into
	// Options.ReportingCurrency, sorted by Kind ascending. A kind whose
	// only account(s) couldn't be converted (see Unconverted) still gets
	// a row here -- summed over whatever of its accounts did convert,
	// possibly zero -- rather than disappearing entirely.
	ByCategory []AccountKindTotal

	// ByCurrency has one row per distinct account currency, each summed
	// in its own currency with no conversion at all, sorted by Currency
	// ascending.
	ByCurrency []CurrencyTotal

	// Unconverted names every account Overall/ByCategory's conversion
	// couldn't cover -- the same accounts an equivalent AccountBalancesQuery
	// would report under AccountBalancesResult.Unconverted. These accounts
	// still contribute to ByCurrency (which needs no conversion) but are
	// excluded from Overall and from their category's total.
	Unconverted []UnconvertedBalance
}

// resolveBalanceTotalsCurrency resolves BalanceTotalsQuery.TargetCurrency:
// override, when the caller sets one (the same "convert into whatever the
// caller asks" override AccountBalancesQuery.TargetCurrency already
// allows), or ActorID's reporting currency resolved through ADR-0004's
// full ladder -- the user's own preference, then the instance default --
// when left blank, mirroring resolveFetchReportingCurrency's exact
// resolution (fetch_fx_rates.go).
func (s *Service) resolveBalanceTotalsCurrency(ctx context.Context, actorID, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	preference, err := s.resolveReportingCurrency(ctx, actorID)
	if err != nil {
		return "", err
	}
	return normalize.Currency("", "", preference, s.Config.DefaultCurrency)
}

// BalanceTotals implements issue #195's totals-overview use case.
func (s *Service) BalanceTotals(ctx context.Context, q BalanceTotalsQuery) (BalanceTotalsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return BalanceTotalsResult{}, err
	}

	currency, err := s.resolveBalanceTotalsCurrency(ctx, q.ActorID, q.TargetCurrency)
	if err != nil {
		return BalanceTotalsResult{}, err
	}
	opts, err := validateAnalyticsOptions(AnalyticsOptions{ReportingCurrency: currency, Policy: q.Policy, PinnedDate: q.PinnedDate})
	if err != nil {
		return BalanceTotalsResult{}, err
	}

	balances, err := s.AccountBalances(ctx, AccountBalancesQuery{
		ActorID:        q.ActorID,
		AsOf:           q.AsOf,
		TargetCurrency: opts.ReportingCurrency,
		Policy:         opts.Policy,
		PinnedDate:     opts.PinnedDate,
	})
	if err != nil {
		return BalanceTotalsResult{}, err
	}

	overall, err := sumConvertedBalances(balances.Balances, opts.ReportingCurrency)
	if err != nil {
		return BalanceTotalsResult{}, err
	}

	categoryMinor := make(map[ledger.AccountKind]int64)
	var kindsPresent []ledger.AccountKind
	seenKind := make(map[ledger.AccountKind]bool)
	currencyTotals := make(map[string]money.Money)
	var currenciesPresent []string

	for _, b := range balances.Balances {
		kind := b.Account.Kind()
		if !seenKind[kind] {
			seenKind[kind] = true
			kindsPresent = append(kindsPresent, kind)
		}
		if b.Converted != nil {
			categoryMinor[kind] += b.Converted.Amount.AmountMinor()
		}

		cur := b.Balance.Currency()
		if existing, ok := currencyTotals[cur]; ok {
			sum, err := existing.Add(b.Balance)
			if err != nil {
				return BalanceTotalsResult{}, errs.New(errs.Internal).Wrap(err)
			}
			currencyTotals[cur] = sum
		} else {
			currencyTotals[cur] = b.Balance
			currenciesPresent = append(currenciesPresent, cur)
		}
	}

	sort.Slice(kindsPresent, func(i, j int) bool { return kindsPresent[i] < kindsPresent[j] })
	byCategory := make([]AccountKindTotal, 0, len(kindsPresent))
	for _, k := range kindsPresent {
		total, err := money.NewMoney(categoryMinor[k], opts.ReportingCurrency)
		if err != nil {
			return BalanceTotalsResult{}, errs.New(errs.Internal).Wrap(err)
		}
		byCategory = append(byCategory, AccountKindTotal{Kind: k, Total: total})
	}

	sort.Strings(currenciesPresent)
	byCurrency := make([]CurrencyTotal, 0, len(currenciesPresent))
	for _, c := range currenciesPresent {
		byCurrency = append(byCurrency, CurrencyTotal{Currency: c, Total: currencyTotals[c]})
	}

	return BalanceTotalsResult{
		AsOf:        balances.AsOf,
		Options:     opts,
		Overall:     overall,
		ByCategory:  byCategory,
		ByCurrency:  byCurrency,
		Unconverted: balances.Unconverted,
	}, nil
}

// errSafeMessage returns err's user-safe message when it's an *errs.Error
// (never its wrapped internal cause), or its plain Error() text otherwise.
// UnconvertedBalance.Reason uses this rather than err.Error() directly so
// it never accidentally leaks an *errs.Error's internal cause chain into a
// field the response surfaces to the user.
func errSafeMessage(err error) string {
	var e *errs.Error
	if errors.As(err, &e) {
		return e.Message
	}
	return err.Error()
}

// sumConvertedBalances sums every AccountBalance's Converted.Amount into
// one net total in currency, skipping any entry with a nil Converted
// (reported separately by the caller as Unconverted rather than dropped
// silently). Liability accounts (credit_card, loan) already reduce the
// total without any special-casing — see BalanceTotalsResult.Overall's own
// doc comment for why a plain signed sum is correct here. Extracted as a
// pure function (issue #196) so overallBalance below and BalanceTotals'
// own already-fetched AccountBalancesResult can both reuse it without
// BalanceTotals paying for a second AccountBalances call just to get a
// number it can already compute from the balances it already has.
func sumConvertedBalances(balances []AccountBalance, currency string) (money.Money, error) {
	var minor int64
	for _, b := range balances {
		if b.Converted != nil {
			minor += b.Converted.Amount.AmountMinor()
		}
	}
	total, err := money.NewMoney(minor, currency)
	if err != nil {
		return money.Money{}, errs.New(errs.Internal).Wrap(err)
	}
	return total, nil
}

// overallBalance computes the net balance across every account as of
// asOf, converted into targetCurrency under policy — issue #196's shared
// core for NetWorthOverTime's per-period figure (BalanceTotals computes
// its own Overall directly from the AccountBalancesResult it already
// fetched, via sumConvertedBalances, rather than calling this and
// fetching a second time). It calls AccountBalances once and sums the
// result via sumConvertedBalances.
func (s *Service) overallBalance(ctx context.Context, actorID, asOf, targetCurrency string, policy ConversionPolicy, pinnedDate string) (money.Money, []UnconvertedBalance, error) {
	balances, err := s.AccountBalances(ctx, AccountBalancesQuery{
		ActorID:        actorID,
		AsOf:           asOf,
		TargetCurrency: targetCurrency,
		Policy:         policy,
		PinnedDate:     pinnedDate,
	})
	if err != nil {
		return money.Money{}, nil, err
	}
	overall, err := sumConvertedBalances(balances.Balances, targetCurrency)
	if err != nil {
		return money.Money{}, nil, err
	}
	return overall, balances.Unconverted, nil
}

// ---- NetWorthOverTime ----

// NetWorthOverTimeQuery computes issue #196's net-worth-over-time use
// case: the total balance across every account (in TargetCurrency),
// plotted at each Granularity period boundary within
// Filter.DateFrom..DateTo — a time series, distinct from BalanceTotals'
// own single point-in-time snapshot.
type NetWorthOverTimeQuery struct {
	ActorID string
	// Filter's only fields that matter here are DateFrom/DateTo, which
	// define the series' overall range — net worth is a whole-ledger
	// figure, not scoped to one account/category/description/etc, so
	// every other TransactionFilterInput dimension is ignored outright
	// (never even inspected, let alone validated).
	Filter         TransactionFilterInput
	TargetCurrency string
	Policy         ConversionPolicy
	PinnedDate     string
	Granularity    Granularity
}

// NetWorthPoint is one period's net worth, computed as of that period's
// own end date (periodKey.To) — "net worth as of the end of this
// week/month/year".
type NetWorthPoint struct {
	Date   domain.Date
	Amount money.Money
}

// NetWorthOverTimeResult is NetWorthOverTime's result: one point per
// period boundary in the requested range, ascending, plus every account
// any period's own conversion couldn't cover. An account is reported at
// most once in Unconverted even if it lacked a rate across multiple
// periods — the first period it turned up unconvertible in — rather than
// once per period, which would just repeat the same shortfall
// redundantly for a chart that only cares "was this account ever
// excluded from a point on this line".
type NetWorthOverTimeResult struct {
	Options     AnalyticsOptions
	Points      []NetWorthPoint
	Unconverted []UnconvertedBalance
}

// NetWorthOverTime implements issue #196's net-worth-over-time use case.
// Each point is computed by its own call to overallBalance (in turn, its
// own AccountBalances call) — one independent balance-as-of-a-date
// computation per period, consistent with ADR-0002's "no cached
// balances, recompute every time" stance elsewhere in this codebase
// rather than an attempt to fold N dates into a single running total.
func (s *Service) NetWorthOverTime(ctx context.Context, q NetWorthOverTimeQuery) (NetWorthOverTimeResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return NetWorthOverTimeResult{}, err
	}

	var fromDate, toDate *domain.Date
	if strings.TrimSpace(q.Filter.DateFrom) != "" {
		d, err := normalize.DateOf(q.Filter.DateFrom, s.Clock, s.Config.UserTimezone)
		if err != nil {
			return NetWorthOverTimeResult{}, err
		}
		fromDate = &d
	}
	if strings.TrimSpace(q.Filter.DateTo) != "" {
		d, err := normalize.DateOf(q.Filter.DateTo, s.Clock, s.Config.UserTimezone)
		if err != nil {
			return NetWorthOverTimeResult{}, err
		}
		toDate = &d
	}
	if fromDate == nil || toDate == nil {
		return NetWorthOverTimeResult{}, errs.New(errs.InvalidInput).
			Explain("Net worth over time needs both \"from\" and \"to\" to define its date range.").
			Field("filter")
	}

	currency, err := s.resolveBalanceTotalsCurrency(ctx, q.ActorID, q.TargetCurrency)
	if err != nil {
		return NetWorthOverTimeResult{}, err
	}
	opts, err := validateAnalyticsOptions(AnalyticsOptions{ReportingCurrency: currency, Policy: q.Policy, PinnedDate: q.PinnedDate})
	if err != nil {
		return NetWorthOverTimeResult{}, err
	}

	granularity, err := validateGranularity(q.Granularity)
	if err != nil {
		return NetWorthOverTimeResult{}, err
	}

	var keys []periodKey
	if granularity == GranularityCustom {
		// Mirrors CashFlow's own custom-granularity handling: the whole
		// requested range is exactly one bucket rather than one of the
		// fixed recurring periods periodKeysInRange would otherwise walk.
		keys = []periodKey{{From: *fromDate, To: *toDate}}
	} else {
		keys = periodKeysInRange(*fromDate, *toDate, granularity)
	}

	points := make([]NetWorthPoint, 0, len(keys))
	seenUnconverted := make(map[string]bool)
	var unconverted []UnconvertedBalance
	for _, k := range keys {
		overall, periodUnconverted, err := s.overallBalance(ctx, q.ActorID, k.To.String(), opts.ReportingCurrency, opts.Policy, opts.PinnedDate)
		if err != nil {
			return NetWorthOverTimeResult{}, err
		}
		points = append(points, NetWorthPoint{Date: k.To, Amount: overall})
		for _, u := range periodUnconverted {
			if seenUnconverted[u.Account.ID()] {
				continue
			}
			seenUnconverted[u.Account.ID()] = true
			unconverted = append(unconverted, u)
		}
	}

	return NetWorthOverTimeResult{Options: opts, Points: points, Unconverted: unconverted}, nil
}
