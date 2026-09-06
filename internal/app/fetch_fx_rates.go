package app

import (
	"context"
	"time"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// maxBackfillRangeDays bounds a single FetchFxRates backfill request: each
// day in the range becomes one row per pair, held entirely in memory before
// the final StoreBatch (see FetchFxRates' doc comment) -- an unbounded
// range would mean an unbounded number of rows in memory at once. Ten years
// comfortably covers a real historical backfill; a caller wanting more
// history makes another call with a later starting range instead of one
// request pulling in decades at once.
const maxBackfillRangeDays = 3653 // 10 years, inclusive of leap days

// FetchFxRatesCommand is issue #135's one explicit, network-touching,
// store-writing FX action -- nothing else in this codebase calls
// Service.FxProvider or writes to fx_rates. Every fetched pair is quoted
// against ActorID's own resolved reporting currency (ADR-0004's currency
// ladder, resolved the same way CreateAccount resolves it): there is no
// caller-supplied quote currency, because fx_rates only ever needs rates
// against whichever currency conversions are ultimately reported in.
type FetchFxRatesCommand struct {
	ActorID string

	// Pairs optionally restricts the fetch to these base ISO 4217 currency
	// codes, each quoted against the resolved reporting currency. Empty
	// (the default) fetches every pair ports.FxRateRepository.InUsePairs
	// reports for ActorID -- the balances-screen "refresh everything"
	// action. A caller with just one or two currencies in view (e.g. the
	// transaction-entry screen) passes exactly those instead of paying for
	// every in-use pair. A base equal to the resolved reporting currency
	// is silently skipped -- there's no rate to fetch for a currency
	// against itself, the same reasoning InUsePairs itself already
	// applies to its own default set.
	Pairs []string

	// From and To optionally bound a historical backfill range (inclusive
	// calendar dates, both resolved via normalize.DateOf). Both must be
	// set together -- setting one without the other is rejected. Leaving
	// both empty fetches only the current/latest date (today, per the
	// service's clock) for each pair, one FxProvider.FetchRate call per
	// pair. Setting both delegates to FxProvider.FetchRange once per pair
	// instead -- never a loop over individual dates in the range
	// (ADR-0012).
	From string
	To   string
}

// FetchedRate is one rate FetchFxRates fetched and stored, echoed back in
// its result so a caller can show what was just fetched without a second
// read.
type FetchedRate struct {
	Rate   fx.Rate
	Date   domain.Date
	Source string
}

// FetchFxRatesResult is FetchFxRates' result: the reporting currency every
// pair was quoted against (so a caller that left it to resolve internally
// can still see what was used), and every row actually fetched and
// stored.
type FetchFxRatesResult struct {
	ReportingCurrency string
	Fetched           []FetchedRate
}

// FetchFxRates implements issue #135's fetch-and-store FX use case. Every
// pair's rows are fetched first, entirely in memory, before anything is
// written -- a provider failure partway through leaves fx_rates completely
// untouched, and the final write is one FxRates.StoreBatch call so a
// multi-pair fetch is atomic (all pairs' rows land, or none do), never a
// partially-applied backfill.
func (s *Service) FetchFxRates(ctx context.Context, cmd FetchFxRatesCommand) (FetchFxRatesResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return FetchFxRatesResult{}, err
	}
	if (cmd.From == "") != (cmd.To == "") {
		return FetchFxRatesResult{}, errs.New(errs.InvalidInput).
			Explain("A backfill range needs both a from and a to date.").
			Field("from")
	}

	reportingCurrency, err := s.resolveFetchReportingCurrency(ctx, cmd.ActorID)
	if err != nil {
		return FetchFxRatesResult{}, err
	}

	pairs, err := s.resolveFetchPairs(ctx, cmd, reportingCurrency)
	if err != nil {
		return FetchFxRatesResult{}, err
	}

	from, to, backfill, err := s.resolveFetchRange(cmd)
	if err != nil {
		return FetchFxRatesResult{}, err
	}

	var rows []ports.FxRateRow
	var fetched []FetchedRate
	for _, pair := range pairs {
		var providerRates []ports.ProviderRate
		if backfill {
			providerRates, err = s.FxProvider.FetchRange(ctx, pair.Base, pair.Quote, from, to)
		} else {
			var pr ports.ProviderRate
			pr, err = s.FxProvider.FetchRate(ctx, pair.Base, pair.Quote, from)
			if err == nil {
				providerRates = []ports.ProviderRate{pr}
			}
		}
		if err != nil {
			return FetchFxRatesResult{}, err
		}

		source := s.FxProvider.Name()
		for _, pr := range providerRates {
			rows = append(rows, ports.FxRateRow{Rate: pr.Rate, Date: pr.Date, Source: source})
			fetched = append(fetched, FetchedRate{Rate: pr.Rate, Date: pr.Date, Source: source})
		}
	}

	if len(rows) > 0 {
		if err := s.FxRates.StoreBatch(ctx, rows); err != nil {
			return FetchFxRatesResult{}, err
		}
	}

	return FetchFxRatesResult{ReportingCurrency: reportingCurrency, Fetched: fetched}, nil
}

// resolveFetchReportingCurrency resolves ActorID's reporting currency all
// the way down ADR-0004's ladder (entry and account rungs don't apply
// here -- there is no entry or account in play, only the actor's own
// preference and the instance default), the same way CreateAccount
// resolves it. Unlike AccountBalancesQuery.TargetCurrency, FetchFxRates
// has no caller-supplied quote currency to fall back on: it needs a
// concrete currency to pass to InUsePairs and to quote every pair
// against, so "no opinion" must still resolve to something real.
func (s *Service) resolveFetchReportingCurrency(ctx context.Context, actorID string) (string, error) {
	preference, err := s.resolveReportingCurrency(ctx, actorID)
	if err != nil {
		return "", err
	}
	return normalize.Currency("", "", preference, s.Config.DefaultCurrency)
}

// resolveFetchPairs resolves cmd.Pairs to the FxRateRepository.CurrencyPair
// pairs to actually fetch, forwarding to InUsePairs for the empty
// (default) case.
func (s *Service) resolveFetchPairs(ctx context.Context, cmd FetchFxRatesCommand, reportingCurrency string) ([]ports.CurrencyPair, error) {
	if len(cmd.Pairs) == 0 {
		return s.FxRates.InUsePairs(ctx, cmd.ActorID, reportingCurrency)
	}

	pairs := make([]ports.CurrencyPair, 0, len(cmd.Pairs))
	for _, base := range cmd.Pairs {
		if _, ok := money.LookupCurrency(base); !ok {
			return nil, errs.New(errs.InvalidInput).Explain("%q is not a known currency.", base).Field("pairs")
		}
		if base == reportingCurrency {
			continue
		}
		pairs = append(pairs, ports.CurrencyPair{Base: base, Quote: reportingCurrency})
	}
	return pairs, nil
}

// resolveFetchRange resolves cmd's From/To into concrete dates: both
// resolved via normalize.DateOf when set (backfill=true), or today (per
// the service's clock) for both when left empty (backfill=false, a
// current-date fetch).
func (s *Service) resolveFetchRange(cmd FetchFxRatesCommand) (from, to domain.Date, backfill bool, err error) {
	if cmd.From == "" {
		today, err := normalize.DateOf("", s.Clock, s.Config.UserTimezone)
		if err != nil {
			return domain.Date{}, domain.Date{}, false, err
		}
		return today, today, false, nil
	}

	from, err = normalize.DateOf(cmd.From, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return domain.Date{}, domain.Date{}, false, err
	}
	to, err = normalize.DateOf(cmd.To, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return domain.Date{}, domain.Date{}, false, err
	}
	if to.Before(from) {
		return domain.Date{}, domain.Date{}, false, errs.New(errs.InvalidInput).
			Explain("The backfill range's to date (%s) is before its from date (%s).", to, from).
			Field("to")
	}
	if days := daysBetween(from, to); days > maxBackfillRangeDays {
		return domain.Date{}, domain.Date{}, false, errs.New(errs.InvalidInput).
			Explain("The backfill range (%s to %s, %d days) is wider than the %d-day maximum. Split it into smaller ranges.", from, to, days, maxBackfillRangeDays).
			Field("to")
	}
	return from, to, true, nil
}

// daysBetween counts the calendar days from from to to (both inclusive
// endpoints of the caller's range), for maxBackfillRangeDays' width check.
// domain.Date carries no arithmetic of its own (ADR-0005: it's a pure
// year/month/day value with no clock) -- going through time.Date's UTC
// midnight for both ends is the standard way to diff two calendar dates
// without a timezone ever entering the calculation.
func daysBetween(from, to domain.Date) int {
	f := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	return int(t.Sub(f).Hours() / 24)
}
