package app

import (
	"context"
	"sort"

	"github.com/anirudhgray/bodger/internal/domain/budgeting"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
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
// Budgets are included as of #246, sorted by ID ascending the same way
// accounts and categories are (see below) rather than in
// ports.BudgetRepository.List's own most-recently-created-first order.
//
// FX rates are included (as of #221) but never restored: fx_rates carries
// no actor/user scoping at all (ADR-0004 -- "a rate between two currencies
// on a given date is the same fact for every user of this bodger
// instance"), and RestoreSnapshot/SnapshotRepository.Replace both operate
// per-actor, with no ownership check that would make sense for a
// globally-shared table. This is a deliberate export-only asymmetry, not
// an oversight: a restore never touches fx_rates, so any rows present
// before a restore remain present, untouched, after it.
//
// RecurringRules and ScheduledOccurrences are included as of #283, sorted
// by ID ascending the same way every other entity here is -- they are
// genuinely per-actor data (unlike FxRates), so they round-trip through
// restore too, on the Budgets side of that distinction.
type ExportSnapshot struct {
	Accounts             []ledger.Account
	Categories           []ledger.Category
	Transactions         []ExportedTransaction
	Budgets              []budgeting.Budget
	FxRates              []ports.FxRateRow
	RecurringRules       []recurring.RecurringRule
	ScheduledOccurrences []recurring.ScheduledOccurrence
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

	budgets, err := s.Budgets.List(ctx, q.ActorID)
	if err != nil {
		return ExportSnapshot{}, err
	}
	// List's own SQL order is most-recently-created-first (its doc
	// comment) -- not the deterministic-by-ID order export needs, so this
	// re-sorts the same way accounts/categories above already do.
	sort.Slice(budgets, func(i, j int) bool { return budgets[i].ID() < budgets[j].ID() })

	fxRates, err := s.FxRates.ListAll(ctx)
	if err != nil {
		return ExportSnapshot{}, err
	}

	recurringRules, err := s.RecurringRules.List(ctx, q.ActorID)
	if err != nil {
		return ExportSnapshot{}, err
	}
	sort.Slice(recurringRules, func(i, j int) bool { return recurringRules[i].ID() < recurringRules[j].ID() })

	// Status: "" (the zero value) matches every status, and RuleID: ""
	// matches every rule -- an actor-wide, all-statuses read, exactly
	// ExportSnapshot's "complete" contract.
	scheduledOccurrences, err := s.ScheduledOccurrences.List(ctx, q.ActorID, ports.ScheduledOccurrenceFilter{})
	if err != nil {
		return ExportSnapshot{}, err
	}
	sort.Slice(scheduledOccurrences, func(i, j int) bool { return scheduledOccurrences[i].ID() < scheduledOccurrences[j].ID() })

	return ExportSnapshot{
		Accounts:             accounts,
		Categories:           categories,
		Transactions:         exported,
		Budgets:              budgets,
		FxRates:              fxRates,
		RecurringRules:       recurringRules,
		ScheduledOccurrences: scheduledOccurrences,
	}, nil
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
