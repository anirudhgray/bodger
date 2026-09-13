package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/budgeting"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	"github.com/anirudhgray/bodger/internal/ports"
)

// RestoreSnapshotQuery is RestoreSnapshot's input: the actor doing the
// restore, and the raw bytes of a canonical JSON document (ADR-0008's
// bodger.export/v1 envelope — exactly what ExportJSON produces) to replace
// their ledger with.
type RestoreSnapshotQuery struct {
	ActorID  string
	Document []byte
}

// RestoreSnapshotResult reports how many rows of each kind the restore
// installed, for a surface to summarise back to the user.
type RestoreSnapshotResult struct {
	Accounts     int
	Categories   int
	Transactions int
	Budgets      int
}

// RestoreSnapshot implements issue #226: a full-state restore of a
// bodger.export/v1 canonical JSON document. It is the opposite operation
// from the staged CSV import pipeline (#210/#211/#212): a canonical export
// is already fully-resolved domain data with real structure, not
// unreviewed external data, so restoring one is a straight atomic replace
// rather than a staged, reviewable commit.
//
// The document's own format version is checked exactly against
// ExportFormatVersion — an unknown or missing version is always rejected,
// never guessed at (ADR-0008 treats the format string as the one thing a
// deliberate version bump changes). Every surrogate ID in the document
// (account, category, transaction, and posting IDs) is regenerated on
// load and every internal reference between them is remapped to point at
// the newly generated IDs instead — ADR-0008's round-trip guarantee
// explicitly allows "Surrogate UUIDs (consistently remapped)" to differ
// across export -> import -> export, which is exactly what happens here.
// The actual wipe-then-reload happens in one database transaction
// (ports.SnapshotRepository.Replace): a failure at any point, in this
// method or inside that transaction, leaves the actor's existing ledger
// completely untouched.
//
// Budgets and their lines are restored (#246), through the same
// regenerate-every-surrogate-ID-and-remap-references discipline as every
// other entity here: a budget line's category_id is remapped through the
// same categoryIDs map restoreTransactions' postings already use. FX rates
// are not restored, deliberately: fx_rates carries no actor/user scoping
// at all (ADR-0004), so there is no per-actor ownership check that would
// make sense for it — see ExportSnapshot's doc comment (export.go) and
// ports.Snapshot's own doc comment for the full reasoning (#221).
func (s *Service) RestoreSnapshot(ctx context.Context, q RestoreSnapshotQuery) (RestoreSnapshotResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return RestoreSnapshotResult{}, err
	}

	var envelope jsonEnvelope
	if err := json.Unmarshal(q.Document, &envelope); err != nil {
		return RestoreSnapshotResult{}, errs.New(errs.InvalidInput).
			Explain("That file isn't a valid backup document.").
			Field("document").
			Wrap(err)
	}
	if envelope.Format != ExportFormatVersion {
		got := envelope.Format
		if got == "" {
			got = "(missing)"
		}
		return RestoreSnapshotResult{}, errs.New(errs.InvalidInput).
			Explain("This file's format is %q, but bodger can only restore %q.", got, ExportFormatVersion).
			Field("document")
	}

	snapshot, err := buildRestoreSnapshot(s.IDs, q.ActorID, envelope)
	if err != nil {
		return RestoreSnapshotResult{}, err
	}

	if err := s.Snapshots.Replace(ctx, q.ActorID, snapshot); err != nil {
		return RestoreSnapshotResult{}, err
	}

	return RestoreSnapshotResult{
		Accounts:     len(snapshot.Accounts),
		Categories:   len(snapshot.Categories),
		Transactions: len(snapshot.Transactions),
		Budgets:      len(snapshot.Budgets),
	}, nil
}

// buildRestoreSnapshot turns envelope's already-parsed wire values into a
// ports.Snapshot: every account, category, and transaction/posting ID
// regenerated via ids, and every reference between them (a posting's
// account/category, a category's parent, a transaction's related
// transaction) remapped to point at the newly generated ID rather than the
// document's own. It fails closed — any unresolved reference, invalid
// field, or structurally invalid domain value is an error naming what
// couldn't be resolved, rather than silently dropping or best-effort
// guessing at it — precisely because this runs entirely before
// SnapshotRepository.Replace is ever called, so failing here can never
// leave a half-applied restore for that transaction to worry about.
func buildRestoreSnapshot(ids idgen.Generator, actorID string, envelope jsonEnvelope) (ports.Snapshot, error) {
	accounts, accountIDs, err := restoreAccounts(ids, actorID, envelope.Accounts)
	if err != nil {
		return ports.Snapshot{}, err
	}

	categories, categoryIDs, err := restoreCategories(ids, actorID, envelope.Categories)
	if err != nil {
		return ports.Snapshot{}, err
	}

	transactions, err := restoreTransactions(ids, actorID, envelope.Transactions, accountIDs, categoryIDs)
	if err != nil {
		return ports.Snapshot{}, err
	}

	budgets, err := restoreBudgets(ids, actorID, envelope.Budgets, categoryIDs)
	if err != nil {
		return ports.Snapshot{}, err
	}

	return ports.Snapshot{Accounts: accounts, Categories: categories, Transactions: transactions, Budgets: budgets}, nil
}

// restoreAccounts rebuilds every account in raw with a freshly generated
// ID, returning the accounts alongside the old-ID -> new-ID map every later
// reference (a posting's account_id) resolves through.
func restoreAccounts(ids idgen.Generator, actorID string, raw []jsonAccount) ([]ledger.Account, map[string]string, error) {
	accountIDs := make(map[string]string, len(raw))
	accounts := make([]ledger.Account, 0, len(raw))

	for _, ja := range raw {
		newID := ids.NewID()
		accountIDs[ja.ID] = newID

		obDate, err := restoreOptionalDate(ja.OpeningBalanceDate)
		if err != nil {
			return nil, nil, restoreFieldError(err, "accounts", ja.ID, "opening_balance_date")
		}
		archivedAt, err := restoreOptionalDate(ja.ArchivedAt)
		if err != nil {
			return nil, nil, restoreFieldError(err, "accounts", ja.ID, "archived_at")
		}

		acc, err := ledger.NewAccount(newID, actorID, ja.Name, ledger.AccountKind(ja.Kind), ja.OpeningBalance, obDate, ja.Institution, ja.SortOrder, archivedAt)
		if err != nil {
			return nil, nil, restoreFieldError(err, "accounts", ja.ID, "")
		}
		accounts = append(accounts, acc)
	}
	return accounts, accountIDs, nil
}

// restoreCategories rebuilds every category in raw with a freshly generated
// ID, remapping parent_id through the same old-ID -> new-ID map it builds,
// and rejects a parent cycle before ever handing anything to
// SnapshotRepository.Replace — a check ledger.NewCategory itself only makes
// for the immediate self-parent case (see its doc comment), and which the
// real CategoryRepository.Create/Update normally make with a live SQL query
// against already-stored rows that don't exist yet here, since a restore
// installs an entire tree in one pass rather than one category at a time.
//
// The returned slice is ordered so every category comes after its own
// parent (a topological order), regardless of raw's own array order —
// issue #237: ExportSnapshot sorts categories by ID for deterministic
// export output (ADR-0008), which has nothing to do with parent/child
// topology, so a child can easily land before its parent in the document.
// SnapshotRepository.Replace inserts categories one row at a time and
// categories.parent_id is a foreign key to categories.id, so handing it a
// child before its parent trips a FOREIGN KEY constraint error.
// checkNoCategoryCycles has already proven raw's parent chains all
// terminate, which is exactly the precondition topologicalCategoryOrder's
// depth-based reordering needs.
func restoreCategories(ids idgen.Generator, actorID string, raw []jsonCategory) ([]ledger.Category, map[string]string, error) {
	if err := checkNoCategoryCycles(raw); err != nil {
		return nil, nil, err
	}

	categoryIDs := make(map[string]string, len(raw))
	for _, jc := range raw {
		categoryIDs[jc.ID] = ids.NewID()
	}

	ordered := topologicalCategoryOrder(raw)

	categories := make([]ledger.Category, 0, len(raw))
	for _, jc := range ordered {
		var newParentID *string
		if jc.ParentID != nil {
			resolved, ok := categoryIDs[*jc.ParentID]
			if !ok {
				return nil, nil, errs.New(errs.InvalidInput).
					Explain("The document's categories entry %q references a parent category %q that doesn't exist in the document.", jc.ID, *jc.ParentID).
					Field("document").
					Wrap(fmt.Errorf("app: category %q references unknown parent %q", jc.ID, *jc.ParentID))
			}
			newParentID = &resolved
		}

		archivedAt, err := restoreOptionalDate(jc.ArchivedAt)
		if err != nil {
			return nil, nil, restoreFieldError(err, "categories", jc.ID, "archived_at")
		}

		cat, err := ledger.NewCategory(categoryIDs[jc.ID], actorID, newParentID, jc.Name, ledger.CategoryKind(jc.Kind), jc.SortOrder, archivedAt)
		if err != nil {
			return nil, nil, restoreFieldError(err, "categories", jc.ID, "")
		}
		categories = append(categories, cat)
	}
	return categories, categoryIDs, nil
}

// checkNoCategoryCycles walks each category's declared parent chain, using
// the document's own IDs before any remapping, and fails if any chain
// doesn't terminate within len(raw) hops — the only way a chain that long
// could still be going is a cycle, since a genuine tree of n nodes has no
// path longer than n-1 edges. ledger.NewCategory only ever rejects the
// immediate self-parent case (its own doc comment says deeper cycles are a
// repository/application-layer concern); the real
// CategoryRepository.checkNoCycle checks a deeper cycle with a live SQL
// walk against already-stored rows, which don't exist yet here — a restore
// installs an entire tree in one pass, so this does the equivalent walk in
// memory, before anything is handed to SnapshotRepository.Replace.
func checkNoCategoryCycles(raw []jsonCategory) error {
	parentOf := make(map[string]*string, len(raw))
	for _, jc := range raw {
		parentOf[jc.ID] = jc.ParentID
	}

	for _, jc := range raw {
		current := jc.ID
		for steps := 0; ; steps++ {
			if steps > len(raw) {
				return errs.New(errs.InvalidInput).
					Explain("The document's categories entry %q is part of a parent cycle.", jc.ID).
					Field("document").
					Wrap(fmt.Errorf("app: category %q is part of a parent cycle", jc.ID))
			}
			parent := parentOf[current]
			if parent == nil {
				break
			}
			current = *parent
		}
	}
	return nil
}

// topologicalCategoryOrder returns raw reordered so every category comes
// after its own parent (parents-before-children), preserving raw's own
// relative order otherwise (a stable sort by parent-chain depth) — the
// fix for issue #237: SnapshotRepository.Replace inserts categories one
// row at a time in whatever order it's handed, and
// categories.parent_id REFERENCES categories(id), so a child inserted
// before its parent row exists fails with a FOREIGN KEY constraint
// error. The document's own array order (raw) carries no such guarantee
// — ExportSnapshot sorts by ID for deterministic export output
// (ADR-0008), unrelated to parent/child topology. This only needs to run
// after checkNoCategoryCycles has already proven every parent chain in
// raw terminates: a category with a longer chain always sorts after
// every category on that chain, which is exactly parents-before-children.
func topologicalCategoryOrder(raw []jsonCategory) []jsonCategory {
	parentOf := make(map[string]*string, len(raw))
	for _, jc := range raw {
		parentOf[jc.ID] = jc.ParentID
	}

	depth := make(map[string]int, len(raw))
	for _, jc := range raw {
		depth[jc.ID] = categoryDepth(jc.ID, parentOf)
	}

	ordered := make([]jsonCategory, len(raw))
	copy(ordered, raw)
	sort.SliceStable(ordered, func(i, j int) bool {
		return depth[ordered[i].ID] < depth[ordered[j].ID]
	})
	return ordered
}

// categoryDepth counts the hops from id up to the root of its parent
// chain (0 for a top-level category, 1 for a direct child, and so on),
// walking parentOf the same way checkNoCategoryCycles does. It's only
// ever called after checkNoCategoryCycles has already proven every chain
// in the document terminates, so this walk doesn't need its own cycle
// guard.
func categoryDepth(id string, parentOf map[string]*string) int {
	depth := 0
	current := id
	for {
		parent := parentOf[current]
		if parent == nil {
			return depth
		}
		depth++
		current = *parent
	}
}

// restoreTransactions rebuilds every transaction (and its postings and
// tags) in raw with freshly generated IDs, remapping each posting's
// account/category and each transaction's related-transaction reference
// through accountIDs/categoryIDs and this function's own transaction-ID
// map.
func restoreTransactions(ids idgen.Generator, actorID string, raw []jsonExportedTxn, accountIDs, categoryIDs map[string]string) ([]ports.SnapshotTransaction, error) {
	txnIDs := make(map[string]string, len(raw))
	for _, jt := range raw {
		txnIDs[jt.ID] = ids.NewID()
	}

	out := make([]ports.SnapshotTransaction, 0, len(raw))
	for _, jt := range raw {
		newID := txnIDs[jt.ID]

		postings, err := restorePostings(ids, jt.Postings, accountIDs, categoryIDs)
		if err != nil {
			return nil, restoreFieldError(err, "transactions", jt.ID, "postings")
		}

		bookedDate, err := parseISODate(jt.BookedDate)
		if err != nil {
			return nil, restoreFieldError(err, "transactions", jt.ID, "booked_date")
		}

		var opts []ledger.TransactionOption
		if jt.Notes != "" {
			opts = append(opts, ledger.WithNotes(jt.Notes))
		}
		if jt.PostedDate != nil {
			postedDate, err := parseISODate(*jt.PostedDate)
			if err != nil {
				return nil, restoreFieldError(err, "transactions", jt.ID, "posted_date")
			}
			opts = append(opts, ledger.WithPostedDate(postedDate))
		}
		importRecordID, externalID := "", ""
		if jt.ImportRecordID != nil {
			importRecordID = *jt.ImportRecordID
		}
		if jt.ExternalID != nil {
			externalID = *jt.ExternalID
		}
		if importRecordID != "" || externalID != "" {
			opts = append(opts, ledger.WithImportProvenance(importRecordID, externalID))
		}
		if jt.RelatedTransactionID != nil {
			relatedNewID, ok := txnIDs[*jt.RelatedTransactionID]
			if !ok {
				return nil, errs.New(errs.InvalidInput).
					Explain("The document's transactions entry %q references a related transaction %q that doesn't exist in the document.", jt.ID, *jt.RelatedTransactionID).
					Field("document").
					Wrap(fmt.Errorf("app: unknown related transaction %q", *jt.RelatedTransactionID))
			}
			opts = append(opts, ledger.WithRelatedTransaction(relatedNewID))
		}

		txn, err := buildRestoredTransaction(newID, actorID, ledger.TransactionKind(jt.Kind), bookedDate, jt.Description, postings, opts)
		if err != nil {
			return nil, restoreFieldError(err, "transactions", jt.ID, "")
		}

		tags := make([]ledger.Tag, 0, len(jt.Tags))
		for _, tagValue := range jt.Tags {
			tag, err := ledger.NewTag(tagValue)
			if err != nil {
				return nil, restoreFieldError(err, "transactions", jt.ID, "tags")
			}
			tags = append(tags, tag)
		}

		out = append(out, ports.SnapshotTransaction{Transaction: txn, Tags: tags})
	}
	return out, nil
}

// buildRestoredTransaction dispatches to the ledger constructor matching
// kind, the same three-way dispatch internal/adapters/sqlite's own
// buildTransaction uses to reconstruct a stored row -- restoring a document
// has to go through the same validating construction any other write does.
// A cross-currency transfer's implied FX rate is re-derived from its two
// postings here (ledger.NewTransfer always computes one) rather than
// restored from a stored value, because the canonical JSON envelope never
// carried fx_rate_used/fx_rate_source to begin with (see jsonExportedTxn) --
// the same two leg amounts always imply the same rate, so nothing is lost.
func buildRestoredTransaction(id, actorID string, kind ledger.TransactionKind, bookedDate domain.Date, description string, postings []ledger.Posting, opts []ledger.TransactionOption) (ledger.Transaction, error) {
	switch kind {
	case ledger.TransactionKindOutflow:
		return ledger.NewOutflow(id, actorID, bookedDate, description, postings, opts...)
	case ledger.TransactionKindInflow:
		return ledger.NewInflow(id, actorID, bookedDate, description, postings, opts...)
	case ledger.TransactionKindTransfer:
		txn, rate, err := ledger.NewTransfer(id, actorID, bookedDate, description, postings, opts...)
		if err != nil {
			return ledger.Transaction{}, err
		}
		if !rate.IsIdentity() {
			txn = txn.WithFxRate(rate, ledger.FxRateSourceImplied)
		}
		return txn, nil
	default:
		return ledger.Transaction{}, errs.New(errs.InvalidInput).
			Explain("%q isn't a transaction type bodger recognizes.", kind).
			Field("document").
			Wrap(fmt.Errorf("app: unknown transaction kind %q", kind))
	}
}

// restorePostings rebuilds one transaction's postings with freshly
// generated IDs, remapping each posting's account_id/category_id through
// accountIDs/categoryIDs -- a dangling reference (an account or category ID
// the document's own accounts/categories section never declared) is an
// error, never silently dropped.
func restorePostings(ids idgen.Generator, raw []jsonPosting, accountIDs, categoryIDs map[string]string) ([]ledger.Posting, error) {
	postings := make([]ledger.Posting, 0, len(raw))
	for _, jp := range raw {
		newAccountID, ok := accountIDs[jp.AccountID]
		if !ok {
			return nil, errs.New(errs.InvalidInput).
				Explain("Posting %q references an account %q that doesn't exist in the document.", jp.ID, jp.AccountID).
				Field("document").
				Wrap(fmt.Errorf("app: posting %q references unknown account %q", jp.ID, jp.AccountID))
		}

		var newCategoryID *string
		if jp.CategoryID != nil {
			resolved, ok := categoryIDs[*jp.CategoryID]
			if !ok {
				return nil, errs.New(errs.InvalidInput).
					Explain("Posting %q references a category %q that doesn't exist in the document.", jp.ID, *jp.CategoryID).
					Field("document").
					Wrap(fmt.Errorf("app: posting %q references unknown category %q", jp.ID, *jp.CategoryID))
			}
			newCategoryID = &resolved
		}

		posting, err := ledger.NewPosting(ids.NewID(), newAccountID, jp.Amount, newCategoryID, jp.SortOrder)
		if err != nil {
			return nil, err
		}
		postings = append(postings, posting)
	}
	return postings, nil
}

// restoreBudgets rebuilds every budget (and its lines) in raw with freshly
// generated IDs — one new ID per budget and one per line, the same
// per-aggregate-and-its-children ID assignment restoreTransactions uses
// for a transaction and its postings. Each line's category_id is remapped
// through categoryIDs, the same map restorePostings already resolves a
// posting's category_id through; a line referencing a category ID the
// document's own categories section never declared is an error, never
// silently dropped, the same as a dangling posting reference.
func restoreBudgets(ids idgen.Generator, actorID string, raw []jsonBudget, categoryIDs map[string]string) ([]budgeting.Budget, error) {
	budgets := make([]budgeting.Budget, 0, len(raw))
	for _, jb := range raw {
		newID := ids.NewID()

		lines := make([]budgeting.BudgetLine, 0, len(jb.Lines))
		for _, jl := range jb.Lines {
			newCategoryID, ok := categoryIDs[jl.CategoryID]
			if !ok {
				return nil, errs.New(errs.InvalidInput).
					Explain("The document's budgets entry %q has a line referencing a category %q that doesn't exist in the document.", jb.ID, jl.CategoryID).
					Field("document").
					Wrap(fmt.Errorf("app: budget %q line references unknown category %q", jb.ID, jl.CategoryID))
			}

			line, err := budgeting.NewBudgetLine(ids.NewID(), newID, newCategoryID, jl.AmountMinor, jl.Rollover)
			if err != nil {
				return nil, restoreFieldError(err, "budgets", jb.ID, "lines")
			}
			lines = append(lines, line)
		}

		startsOn, err := parseISODate(jb.StartsOn)
		if err != nil {
			return nil, restoreFieldError(err, "budgets", jb.ID, "starts_on")
		}

		var opts []budgeting.BudgetOption
		if jb.ArchivedAt != nil {
			archivedAt, err := parseISODate(*jb.ArchivedAt)
			if err != nil {
				return nil, restoreFieldError(err, "budgets", jb.ID, "archived_at")
			}
			opts = append(opts, budgeting.WithArchivedAt(archivedAt))
		}

		budget, err := budgeting.NewBudget(newID, actorID, jb.Name, budgeting.PeriodType(jb.PeriodType), jb.Currency, startsOn, lines, opts...)
		if err != nil {
			return nil, restoreFieldError(err, "budgets", jb.ID, "")
		}
		budgets = append(budgets, budget)
	}
	return budgets, nil
}

// restoreOptionalDate parses raw (an ISO 8601 date string) if non-nil,
// returning nil for a nil raw -- the "(value, bool)" -> "*T" bridge every
// optional date field on jsonAccount/jsonCategory/jsonExportedTxn needs.
func restoreOptionalDate(raw *string) (*domain.Date, error) {
	if raw == nil {
		return nil, nil
	}
	d, err := parseISODate(*raw)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// parseISODate parses raw as a strict "YYYY-MM-DD" calendar date -- the
// exact format ExportJSON writes every date field in (domain.Date.String's
// own format). This deliberately doesn't go through
// internal/app/normalize.DateOf: that function treats an empty string as
// "today", which is exactly wrong here -- a restored document's dates are
// already-resolved facts, never subject to "today" defaulting, and an
// empty or malformed date in the document is a reason to reject the whole
// restore, not to quietly substitute the current date.
func parseISODate(raw string) (domain.Date, error) {
	t, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return domain.Date{}, errs.New(errs.InvalidInput).
			Explain("%q isn't a valid calendar date.", raw).
			Field("document").
			Wrap(fmt.Errorf("app: parse date %q: %w", raw, err))
	}
	return domain.NewDate(t.Year(), t.Month(), t.Day())
}

// restoreFieldError wraps err into an *errs.Error naming which section,
// document ID, and (optionally) field the failure came from -- every
// rejection this file produces stays specific enough to actually debug a
// bad document, without ever citing this package's own internal structure.
//
// If err already wraps an *errs.Error -- every reference-resolution error
// this file builds above does, and so does a domain constructor error by
// the time wrapLedgerError-style callers reach it -- it's returned as-is
// rather than double-wrapped (the same rule wrapLedgerError follows): its
// own message is already specific to exactly what was wrong, which a
// second, more generic "entry %q isn't valid" wrapper could only make
// vaguer.
func restoreFieldError(err error, section, id, field string) error {
	var e *errs.Error
	if errors.As(err, &e) {
		return e
	}
	if field != "" {
		return errs.New(errs.InvalidInput).
			Explain("The document's %s entry %q has an invalid %s.", section, id, field).
			Field("document").
			Wrap(err)
	}
	return errs.New(errs.InvalidInput).
		Explain("The document's %s entry %q isn't valid.", section, id).
		Field("document").
		Wrap(err)
}
