package app

import (
	"context"

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
type AccountBalancesQuery struct {
	ActorID string
	AsOf    string
}

// AccountBalance pairs one Account with its computed balance as of the
// query's AsOf date.
type AccountBalance struct {
	Account ledger.Account
	Balance money.Money
}

// AccountBalancesResult is AccountBalances' result: the resolved AsOf date
// (so a caller that passed "" or "today" can see what date it actually
// resolved to), and every account's balance, in the same name order
// ports.AccountRepository.List returns.
type AccountBalancesResult struct {
	AsOf     domain.Date
	Balances []AccountBalance
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

	balances := make([]AccountBalance, 0, len(accounts))
	for _, account := range accounts {
		balance, err := ledger.Balance(account, asOf, txns)
		if err != nil {
			return AccountBalancesResult{}, errs.New(errs.Internal).Wrap(err)
		}
		balances = append(balances, AccountBalance{Account: account, Balance: balance})
	}
	return AccountBalancesResult{AsOf: asOf, Balances: balances}, nil
}
