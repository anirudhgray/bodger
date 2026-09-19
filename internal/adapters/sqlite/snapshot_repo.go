package sqlite

import (
	"context"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// requireOccurrenceRuleInSnapshot returns a *errs.Error with code
// NotAllowed if occurrence's RuleID isn't one of ruleIDs — the check that
// stands in for a per-row ownership lookup an occurrence (which carries no
// UserID of its own) can't otherwise make: Replace's caller-level
// ownership check on snapshot.RecurringRules already guarantees every ID
// in ruleIDs belongs to actorID, so an occurrence referencing anything
// else is a snapshot that was never internally consistent to begin with.
func requireOccurrenceRuleInSnapshot(ruleIDs map[string]bool, ruleID string) *errs.Error {
	if !ruleIDs[ruleID] {
		return errs.New(errs.NotAllowed).Explain("A scheduled occurrence references a recurring rule not present in this snapshot.")
	}
	return nil
}

// SnapshotRepository implements ports.SnapshotRepository over a *DB.
type SnapshotRepository struct {
	db *DB
}

// NewSnapshotRepository constructs a SnapshotRepository backed by db.
func NewSnapshotRepository(db *DB) *SnapshotRepository {
	return &SnapshotRepository{db: db}
}

var _ ports.SnapshotRepository = (*SnapshotRepository)(nil)

// Replace implements ports.SnapshotRepository. It wipes every account,
// category, transaction (with its postings and tags), budget (with its
// lines), and recurring rule (with its scheduled occurrences) actorID
// currently owns, then writes snapshot's rows in their place, all inside
// one write transaction — a failure anywhere in this method rolls the
// whole thing back via the deferred Rollback below, leaving actorID's
// prior data exactly as it was (issue #226's atomicity requirement;
// recurring rules/occurrences added by #283).
func (r *SnapshotRepository) Replace(ctx context.Context, actorID string, snapshot ports.Snapshot) error {
	if err := requireActorID(actorID); err != nil {
		return err
	}
	for _, a := range snapshot.Accounts {
		if err := requireActor(actorID, a.UserID()); err != nil {
			return err
		}
	}
	for _, c := range snapshot.Categories {
		if err := requireActor(actorID, c.UserID()); err != nil {
			return err
		}
	}
	for _, st := range snapshot.Transactions {
		if err := requireActor(actorID, st.Transaction.UserID()); err != nil {
			return err
		}
	}
	for _, b := range snapshot.Budgets {
		if err := requireActor(actorID, b.UserID()); err != nil {
			return err
		}
	}
	ruleIDs := make(map[string]bool, len(snapshot.RecurringRules))
	for _, rule := range snapshot.RecurringRules {
		if err := requireActor(actorID, rule.UserID()); err != nil {
			return err
		}
		ruleIDs[rule.ID()] = true
	}
	for _, o := range snapshot.ScheduledOccurrences {
		if err := requireOccurrenceRuleInSnapshot(ruleIDs, o.RuleID()); err != nil {
			return err
		}
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	if err := wipeActorLedger(ctx, tx, actorID); err != nil {
		return err
	}

	now := formatTime(r.db.clock.Now())

	for _, a := range snapshot.Accounts {
		if err := insertAccountRow(ctx, tx, actorID, a, now); err != nil {
			return err
		}
	}
	for _, c := range snapshot.Categories {
		if err := insertCategoryRow(ctx, tx, actorID, c, now); err != nil {
			return err
		}
	}
	// Budgets are inserted after categories, and reuse insertBudgetRow/
	// insertBudgetLines (budget_repo.go) rather than duplicating their SQL
	// here: budget_lines.category_id must reference an already-existing
	// categories row, so this order matters the same way
	// wipeActorLedger's own ordering below does for the delete side.
	for _, b := range snapshot.Budgets {
		if err := insertBudgetRow(ctx, tx, actorID, b, now); err != nil {
			return err
		}
		if err := insertBudgetLines(ctx, tx, b.ID(), b.Lines()); err != nil {
			return err
		}
	}
	for _, st := range snapshot.Transactions {
		if err := insertTransactionRow(ctx, tx, actorID, st.Transaction, now); err != nil {
			return err
		}
		if err := insertPostings(ctx, tx, st.Transaction.ID(), st.Transaction.Postings()); err != nil {
			return err
		}
		if err := setTransactionTags(ctx, tx, actorID, st.Transaction.ID(), st.Tags, now); err != nil {
			return err
		}
	}
	// Recurring rules are inserted after accounts/categories (account_id/
	// category_id reference them, plain, no cascade) and scheduled
	// occurrences after both rules (rule_id) and transactions
	// (transaction_id, for a materialised one) — the issue's own wording:
	// "transactions before the occurrences that reference them", the same
	// reasoning #240 already established for category topology.
	for _, rule := range snapshot.RecurringRules {
		if err := insertRecurringRuleRow(ctx, tx, actorID, rule, now); err != nil {
			return err
		}
	}
	for _, o := range snapshot.ScheduledOccurrences {
		if err := insertScheduledOccurrenceRow(ctx, tx, o, now); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// wipeActorLedger deletes actorID's existing accounts, categories,
// transactions, budgets, and recurring rules/occurrences through tx, in
// the one order that satisfies every foreign key involved:
//
//  1. import_batch first (cascading import_record via migration 00012's
//     ON DELETE CASCADE) — a batch's target_account_id and a record's
//     transaction_id are plain, non-cascading REFERENCES into
//     accounts/transactions with no cascade of their own, and neither
//     column is cleared by committing, excluding, or rolling back a batch
//     (rollback only soft-deletes the transaction a record points at, per
//     ADR-0008's "provenance stays queryable" guarantee). A restore is a
//     full-ledger replace, so actorID's entire import history — staged,
//     committed, or rolled back — goes with it rather than surviving as
//     orphaned provenance for accounts/transactions that no longer exist
//     (issue #236).
//  2. recurring_rules (cascading scheduled_occurrences via migration
//     00016's ON DELETE CASCADE on rule_id) — added by #283, and for
//     exactly the same reason import_batch has to go first:
//     scheduled_occurrences.transaction_id is a plain, non-cascading
//     REFERENCES into transactions with no cascade of its own, so a
//     materialised occurrence still pointing at a transaction actorID is
//     about to lose would otherwise leave a dangling reference. Deleting
//     recurring_rules here removes its scheduled_occurrences in the same
//     statement, so no separate scheduled_occurrences delete is needed —
//     order relative to import_batch doesn't matter, only that both
//     precede step 4.
//  3. transaction_revisions — it has no ON DELETE CASCADE from
//     transactions (unlike postings and transaction_tags, which do), so a
//     revision row referencing a transaction actorID is about to lose would
//     otherwise leave a dangling reference.
//  4. transactions — cascades postings and transaction_tags automatically
//     (migrations 00006 and 00007's ON DELETE CASCADE).
//  5. budgets — cascades budget_lines automatically (migration 00014's ON
//     DELETE CASCADE on budget_id). This must run before categories are
//     deleted: budget_lines.category_id is a plain REFERENCES categories(id)
//     with no cascade of its own (migration 00014), so deleting a category
//     while a not-yet-deleted budget_line still references it would trip a
//     FOREIGN KEY constraint error. Deleting budgets here removes its
//     budget_lines in the same statement, so no separate budget_lines
//     delete is needed.
//  6. categories, then accounts — nothing above references either by the
//     time this runs, since every posting that could have was already
//     removed by transactions' own cascade, every budget_line by budgets'
//     own cascade in step 5, and recurring_rules (which also references
//     both, plain, no cascade) was already removed in step 2.
func wipeActorLedger(ctx context.Context, tx execer, actorID string) *errs.Error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM import_batch WHERE user_id = ?`, actorID); err != nil {
		return wrapWriteError(err).Explain("Couldn't clear existing import history before restoring.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM recurring_rules WHERE user_id = ?`, actorID); err != nil {
		return wrapWriteError(err).Explain("Couldn't clear existing recurring rules before restoring.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM transaction_revisions WHERE user_id = ?`, actorID); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM transactions WHERE user_id = ?`, actorID); err != nil {
		return wrapWriteError(err).Explain("Couldn't clear existing transactions before restoring.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM budgets WHERE user_id = ?`, actorID); err != nil {
		return wrapWriteError(err).Explain("Couldn't clear existing budgets before restoring.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM categories WHERE user_id = ?`, actorID); err != nil {
		return wrapWriteError(err).Explain("Couldn't clear existing categories before restoring.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM accounts WHERE user_id = ?`, actorID); err != nil {
		return wrapWriteError(err).Explain("Couldn't clear existing accounts before restoring.")
	}
	return nil
}
