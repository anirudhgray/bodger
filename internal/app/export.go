package app

import (
	"context"
	"sort"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// ExportFormatVersion is the versioned envelope identifier ADR-0008
// specifies for the canonical JSON export ("independent of the database
// schema... a format change is a deliberate version bump"). It is a
// standalone constant, not derived from anything else, so bumping it is
// always a conscious edit to this one line.
const ExportFormatVersion = "bodger.export/v1"

// ExportSnapshotQuery fetches issue #209's full domain snapshot for one
// actor: every account, category, and non-deleted transaction (with its
// postings and tags) that actor owns. It is the shared read both
// ExportJSON and ExportCSV build on, so the two formats can never disagree
// about which rows exist -- only about how those rows are written out.
//
// Filter narrows which transactions are included (accounts and categories
// are always exported in full, regardless of Filter -- a filtered export
// still needs every account/category name a matching transaction's
// postings reference). Its zero value is ports.TransactionFilter's own
// "match everything" zero value, so ExportJSON's full, unfiltered backup
// (ADR-0008) and ExportCSV's optionally-filtered download (ADR-0009) share
// this one query struct and this one read, differing only in what Filter
// they pass.
type ExportSnapshotQuery struct {
	ActorID string
	Filter  ports.TransactionFilter
}

// ExportedTransaction pairs one ledger.Transaction with its Tags.
// ports.TransactionRepository.List (unlike Get) does not load tags -- "It
// does not load tags — callers that need them call Get" -- so
// ExportSnapshot calls Get once per transaction to attach them. This is an
// accepted N+1 rather than a reason to add a bulk-tags method to
// ports.TransactionRepository: export is not a hot path, and this issue's
// own scope is app-layer-only, reading the ports interface as it already
// exists rather than extending it.
type ExportedTransaction struct {
	Transaction ledger.Transaction
	Tags        []ledger.Tag
}

// ExportSnapshot is ExportSnapshotQuery's result: ADR-0008's full domain
// snapshot, in a fixed, deterministic order that depends only on the
// exported rows' own IDs -- never on a repository's incidental return
// order -- so the same underlying data always assembles into the same
// snapshot, byte for byte, regardless of what order List happens to
// return rows in.
//
// Budgets and budget lines are absent: M7 (Budgets) hasn't shipped, so
// internal/domain has no budget domain type to read yet (confirmed against
// internal/domain and internal/app at the time this was written) --
// nothing here invents a placeholder for them. FX rates are also absent:
// ports.FxRateRepository exposes Store, StoreBatch, Lookup (a single
// nearest-earlier-within-a-window resolution for one pair and date), and
// InUsePairs (which pairs are in use, not their stored history) -- there
// is no method that enumerates every stored fx_rate row, and adding one
// would mean extending internal/ports, which this issue's own scope keeps
// out of (app-layer-only, to stay clear of the concurrently-developed
// import-persistence issue's ports changes). Both are gaps to close in a
// follow-up once the missing pieces (the M7 budget domain type; an
// enumerating FX-rate-repository method) exist, not something to
// approximate here.
type ExportSnapshot struct {
	Accounts     []ledger.Account
	Categories   []ledger.Category
	Transactions []ExportedTransaction
}

// ExportSnapshot implements issue #209's full-domain-snapshot read.
func (s *Service) ExportSnapshot(ctx context.Context, q ExportSnapshotQuery) (ExportSnapshot, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ExportSnapshot{}, err
	}

	accounts, err := s.Accounts.List(ctx, q.ActorID)
	if err != nil {
		return ExportSnapshot{}, err
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID() < accounts[j].ID() })

	categories, err := s.Categories.List(ctx, q.ActorID)
	if err != nil {
		return ExportSnapshot{}, err
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i].ID() < categories[j].ID() })

	// q.Filter's zero value matches every non-deleted transaction the
	// actor owns, unpaginated (ports.TransactionFilter's own doc comment)
	// -- exactly ADR-0008's "complete" snapshot when a caller (ExportJSON)
	// leaves it unset, the same unfiltered-unpaginated pattern
	// AccountBalances already relies on for the same reason. A caller that
	// sets Filter (ExportCSV) gets the same query narrowed to a subset of
	// transactions instead.
	txns, err := s.Transactions.List(ctx, q.ActorID, q.Filter)
	if err != nil {
		return ExportSnapshot{}, err
	}

	exported := make([]ExportedTransaction, 0, len(txns))
	for _, txn := range txns {
		// List doesn't load tags; Get does. See ExportedTransaction's doc
		// comment for why this per-transaction fetch is accepted here
		// rather than a reason to extend the port.
		withTags, tags, err := s.Transactions.Get(ctx, q.ActorID, txn.ID())
		if err != nil {
			return ExportSnapshot{}, errs.New(errs.Internal).Wrap(err)
		}
		exported = append(exported, ExportedTransaction{Transaction: withTags, Tags: tags})
	}
	sort.Slice(exported, func(i, j int) bool {
		return exported[i].Transaction.ID() < exported[j].Transaction.ID()
	})

	return ExportSnapshot{Accounts: accounts, Categories: categories, Transactions: exported}, nil
}

// sortedPostings returns txn's postings ordered by SortOrder, then by ID as
// a tiebreak -- deterministic even in the (currently unreachable in
// practice) case of two postings sharing a SortOrder, and independent of
// whatever order the repository that produced txn happened to return them
// in. Both writeJSON and writeCSV call this rather than txn.Postings()
// directly, so the two formats can never list one transaction's postings
// in different orders from each other.
func sortedPostings(txn ledger.Transaction) []ledger.Posting {
	postings := txn.Postings()
	sort.SliceStable(postings, func(i, j int) bool {
		if postings[i].SortOrder() != postings[j].SortOrder() {
			return postings[i].SortOrder() < postings[j].SortOrder()
		}
		return postings[i].ID() < postings[j].ID()
	})
	return postings
}
