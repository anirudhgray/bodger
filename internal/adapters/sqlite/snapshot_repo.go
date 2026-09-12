package sqlite

import (
	"context"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

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
// category, and transaction (with its postings and tags) actorID currently
// owns, then writes snapshot's rows in their place, all inside one write
// transaction — a failure anywhere in this method rolls the whole thing
// back via the deferred Rollback below, leaving actorID's prior data
// exactly as it was (issue #226's atomicity requirement).
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

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// wipeActorLedger deletes actorID's existing accounts, categories, and
// transactions through tx, in the one order that satisfies every foreign
// key involved:
//
//  1. transaction_revisions first — it has no ON DELETE CASCADE from
//     transactions (unlike postings and transaction_tags, which do), so a
//     revision row referencing a transaction actorID is about to lose would
//     otherwise leave a dangling reference.
//  2. transactions — cascades postings and transaction_tags automatically
//     (migrations 00006 and 00007's ON DELETE CASCADE).
//  3. categories, then accounts — nothing above references either by the
//     time this runs, since every posting that could have was already
//     removed by transactions' own cascade.
//
// A staged-but-uncommitted import batch or record referencing one of
// actorID's accounts/categories/transactions (internal/domain/importing,
// out of this issue's scope) has no cascade of its own here and will make
// the delete that would orphan it fail with a foreign-key error instead of
// silently succeeding — which, inside this same transaction, means the
// whole restore rolls back rather than leaving a half-wiped ledger.
func wipeActorLedger(ctx context.Context, tx execer, actorID string) *errs.Error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM transaction_revisions WHERE user_id = ?`, actorID); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM transactions WHERE user_id = ?`, actorID); err != nil {
		return wrapWriteError(err).Explain("Couldn't clear existing transactions before restoring.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM categories WHERE user_id = ?`, actorID); err != nil {
		return wrapWriteError(err).Explain("Couldn't clear existing categories before restoring.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM accounts WHERE user_id = ?`, actorID); err != nil {
		return wrapWriteError(err).Explain("Couldn't clear existing accounts before restoring.")
	}
	return nil
}
