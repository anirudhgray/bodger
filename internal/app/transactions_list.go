package app

import (
	"context"
	"strings"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

const (
	// defaultTransactionListLimit is ListTransactions' page size when the
	// caller doesn't specify one. This default lives here, not in
	// ports.TransactionFilter — that port's Limit <= 0 means "no limit" at
	// the low level (AccountBalances relies on exactly that to fetch every
	// matching transaction), so the "sensible page size" default has to be
	// applied above the port, once, here.
	defaultTransactionListLimit = 50
	// maxTransactionListLimit caps how large a single page can be, so a
	// caller can't accidentally (or deliberately) ask for an unbounded
	// result set through the paginated use case — AccountBalances remains
	// the correct way to get "everything".
	maxTransactionListLimit = 200
)

// GetTransactionQuery fetches a single transaction (with its tags) by ID,
// scoped to the actor (ADR-0006). EditTransaction and DeleteTransaction
// already call TransactionRepository.Get directly for their own purposes;
// this is the same lookup exposed as its own use case, for the REST API's
// GET /api/v1/transactions/{id} (issue #8).
type GetTransactionQuery struct {
	ActorID        string
	TransactionRef string
}

// GetTransaction implements the "fetch one" use case
// GET /api/v1/transactions/{id} needs.
func (s *Service) GetTransaction(ctx context.Context, q GetTransactionQuery) (TransactionResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return TransactionResult{}, err
	}
	if strings.TrimSpace(q.TransactionRef) == "" {
		return TransactionResult{}, errs.New(errs.InvalidInput).Explain("A transaction ID is required.").Field("transaction_ref")
	}
	txn, tags, err := s.Transactions.Get(ctx, q.ActorID, q.TransactionRef)
	if err != nil {
		return TransactionResult{}, err
	}
	return TransactionResult{Transaction: txn, Tags: tags}, nil
}

// ListTransactionsQuery is issue #6's reduced M1 filter: a date range,
// account, category (subtree included by default — ADR-0009, not
// optional), and kind, offset-paginated with a fully-specified sort. The
// full ADR-0009 filter (amount range, tags, currencies, description
// search, import batch) is M4 scope.
type ListTransactionsQuery struct {
	ActorID     string
	AccountRef  string
	CategoryRef string
	Kind        string
	DateFrom    string
	DateTo      string
	Limit       int
	Offset      int
}

// ListTransactionsResult is ListTransactions' result: every matching
// transaction, sorted (booked_date DESC, created_at DESC, id DESC) —
// ADR-0009's fully-specified sort, so the same query run twice returns the
// same rows in the same order, including when booked_date and created_at
// tie.
type ListTransactionsResult struct {
	Transactions []ledger.Transaction
}

// ListTransactions implements issue #6's ListTransactions use case.
// DateFrom and DateTo both resolve through normalize.DateOf, so they're
// interpreted in the actor's configured timezone — ADR-0005 and
// data-model.md §9's "the same query returns identical results under
// TZ=UTC and TZ=Asia/Kolkata" applies here as much as it does to a
// transaction's own booked date.
func (s *Service) ListTransactions(ctx context.Context, q ListTransactionsQuery) (ListTransactionsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ListTransactionsResult{}, err
	}

	var filter ports.TransactionFilter

	if strings.TrimSpace(q.AccountRef) != "" {
		account, err := s.resolveOwnedAccount(ctx, q.ActorID, q.AccountRef)
		if err != nil {
			return ListTransactionsResult{}, attachField(err, "account_ref")
		}
		filter.AccountID = account.ID()
	}

	if strings.TrimSpace(q.CategoryRef) != "" {
		category, err := s.resolveOwnedCategory(ctx, q.ActorID, q.CategoryRef)
		if err != nil {
			return ListTransactionsResult{}, attachField(err, "category_ref")
		}
		filter.CategoryID = category.ID()
	}

	if strings.TrimSpace(q.Kind) != "" {
		kind, err := parseTransactionKind(q.Kind)
		if err != nil {
			return ListTransactionsResult{}, err
		}
		filter.Kind = kind
	}

	if strings.TrimSpace(q.DateFrom) != "" {
		d, err := normalize.DateOf(q.DateFrom, s.Clock, s.Config.UserTimezone)
		if err != nil {
			return ListTransactionsResult{}, err
		}
		filter.FromDate = &d
	}
	if strings.TrimSpace(q.DateTo) != "" {
		d, err := normalize.DateOf(q.DateTo, s.Clock, s.Config.UserTimezone)
		if err != nil {
			return ListTransactionsResult{}, err
		}
		filter.ToDate = &d
	}

	filter.Limit = q.Limit
	if filter.Limit <= 0 {
		filter.Limit = defaultTransactionListLimit
	}
	if filter.Limit > maxTransactionListLimit {
		filter.Limit = maxTransactionListLimit
	}
	filter.Offset = q.Offset
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	txns, err := s.Transactions.List(ctx, q.ActorID, filter)
	if err != nil {
		return ListTransactionsResult{}, err
	}
	return ListTransactionsResult{Transactions: txns}, nil
}
