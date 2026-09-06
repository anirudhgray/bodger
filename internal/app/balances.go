package app

import (
	"context"
	"errors"

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
