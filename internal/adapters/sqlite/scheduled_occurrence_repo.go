package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// ScheduledOccurrenceRepository implements
// ports.ScheduledOccurrenceRepository over a *DB.
//
// Every statement here reaches the actor through the occurrence's rule
// (`JOIN recurring_rules` on a read, an ownership subquery on a write)
// rather than a user_id column on scheduled_occurrences itself — the same
// scope-through-the-parent shape postings use against transactions, and
// the reason there is no second owner column to disagree with the first
// (ADR-0014).
type ScheduledOccurrenceRepository struct {
	db *DB
}

// NewScheduledOccurrenceRepository constructs a
// ScheduledOccurrenceRepository backed by db.
func NewScheduledOccurrenceRepository(db *DB) *ScheduledOccurrenceRepository {
	return &ScheduledOccurrenceRepository{db: db}
}

var _ ports.ScheduledOccurrenceRepository = (*ScheduledOccurrenceRepository)(nil)

const scheduledOccurrenceColumns = `o.id, o.rule_id, o.occurrence_date, o.status, o.transaction_id`

// CreateBatch implements ports.ScheduledOccurrenceRepository.
func (r *ScheduledOccurrenceRepository) CreateBatch(ctx context.Context, actorID string, occurrences []recurring.ScheduledOccurrence) error {
	if err := requireActorID(actorID); err != nil {
		return err
	}
	if len(occurrences) == 0 {
		return nil
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	now := formatTime(r.db.clock.Now())
	for _, o := range occurrences {
		if err := requireRuleOwnedByActor(ctx, tx, actorID, o.RuleID()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO scheduled_occurrences (id, rule_id, occurrence_date, status, transaction_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`,
			o.ID(), o.RuleID(), formatDate(o.OccurrenceDate()), string(o.Status()),
			nullableString(o.TransactionID()), now, now,
		)
		if err != nil {
			return wrapWriteError(err).Explain("Couldn't save the scheduled occurrence for %s.", o.OccurrenceDate())
		}
	}

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// Get implements ports.ScheduledOccurrenceRepository.
func (r *ScheduledOccurrenceRepository) Get(ctx context.Context, actorID, id string) (recurring.ScheduledOccurrence, error) {
	if err := requireActorID(actorID); err != nil {
		return recurring.ScheduledOccurrence{}, err
	}

	row := r.db.read.QueryRowContext(ctx, `
		SELECT `+scheduledOccurrenceColumns+`
		FROM scheduled_occurrences o
		JOIN recurring_rules r ON r.id = o.rule_id
		WHERE o.id = ? AND r.user_id = ?
	`, id, actorID)

	oc, err := scanScheduledOccurrenceRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return recurring.ScheduledOccurrence{}, errs.New(errs.NotFound).
			Explain("No scheduled occurrence with ID %q.", id).Field("id")
	}
	if err != nil {
		return recurring.ScheduledOccurrence{}, errs.New(errs.Internal).Wrap(err)
	}

	occurrence, err := buildScheduledOccurrence(oc)
	if err != nil {
		return recurring.ScheduledOccurrence{}, errs.New(errs.Internal).Wrap(err)
	}
	return occurrence, nil
}

// List implements ports.ScheduledOccurrenceRepository.
func (r *ScheduledOccurrenceRepository) List(ctx context.Context, actorID string, filter ports.ScheduledOccurrenceFilter) ([]recurring.ScheduledOccurrence, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}

	clauses := []string{"r.user_id = ?"}
	args := []any{actorID}
	if filter.RuleID != "" {
		clauses = append(clauses, "o.rule_id = ?")
		args = append(args, filter.RuleID)
	}
	if filter.Status != "" {
		clauses = append(clauses, "o.status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.FromDate != nil {
		clauses = append(clauses, "o.occurrence_date >= ?")
		args = append(args, formatDate(*filter.FromDate))
	}
	if filter.ToDate != nil {
		clauses = append(clauses, "o.occurrence_date <= ?")
		args = append(args, formatDate(*filter.ToDate))
	}

	rows, err := r.db.read.QueryContext(ctx, `
		SELECT `+scheduledOccurrenceColumns+`
		FROM scheduled_occurrences o
		JOIN recurring_rules r ON r.id = o.rule_id
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY o.occurrence_date, o.id
	`, args...)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var occurrences []recurring.ScheduledOccurrence
	for rows.Next() {
		oc, err := scanScheduledOccurrenceRow(rows)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		occurrence, err := buildScheduledOccurrence(oc)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		occurrences = append(occurrences, occurrence)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	return occurrences, nil
}

// Update implements ports.ScheduledOccurrenceRepository.
func (r *ScheduledOccurrenceRepository) Update(ctx context.Context, actorID string, occurrence recurring.ScheduledOccurrence) error {
	if err := requireActorID(actorID); err != nil {
		return err
	}

	now := formatTime(r.db.clock.Now())
	result, err := r.db.write.ExecContext(ctx, `
		UPDATE scheduled_occurrences
		SET occurrence_date = ?, status = ?, transaction_id = ?, updated_at = ?
		WHERE id = ?
		  AND rule_id IN (SELECT id FROM recurring_rules WHERE user_id = ?)
	`,
		formatDate(occurrence.OccurrenceDate()), string(occurrence.Status()),
		nullableString(occurrence.TransactionID()), now,
		occurrence.ID(), actorID,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't update the scheduled occurrence for %s.", occurrence.OccurrenceDate())
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).
			Explain("No scheduled occurrence with ID %q.", occurrence.ID()).Field("id")
	}
	return nil
}

// DeletePending implements ports.ScheduledOccurrenceRepository. The
// status predicate is part of the statement, not the caller's
// responsibility: a materialised or skipped occurrence is never swept away
// by a regeneration (ADR-0014).
func (r *ScheduledOccurrenceRepository) DeletePending(ctx context.Context, actorID, ruleID string, onOrAfter domain.Date) error {
	if err := requireActorID(actorID); err != nil {
		return err
	}

	_, err := r.db.write.ExecContext(ctx, `
		DELETE FROM scheduled_occurrences
		WHERE rule_id = ?
		  AND status = ?
		  AND occurrence_date >= ?
		  AND rule_id IN (SELECT id FROM recurring_rules WHERE user_id = ?)
	`, ruleID, string(recurring.OccurrenceStatusPending), formatDate(onOrAfter), actorID)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// --- shared helpers ---

// requireRuleOwnedByActor refuses a write against a rule the actor doesn't
// own. An occurrence carries no user_id of its own, so this is where
// CreateBatch's ADR-0006 ownership check happens — as a real lookup rather
// than a comparison against a field the entity doesn't have.
func requireRuleOwnedByActor(ctx context.Context, q execer, actorID, ruleID string) *errs.Error {
	var owner string
	err := q.QueryRowContext(ctx, `SELECT user_id FROM recurring_rules WHERE id = ?`, ruleID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return errs.New(errs.NotFound).Explain("No recurring rule with ID %q.", ruleID).Field("rule_id")
	}
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if owner != actorID {
		return errs.New(errs.NotAllowed).Explain("You may not act on another user's data.")
	}
	return nil
}

// scheduledOccurrenceRow is the raw column set
// scanScheduledOccurrenceRow reads before buildScheduledOccurrence turns
// it into a domain recurring.ScheduledOccurrence.
type scheduledOccurrenceRow struct {
	id             string
	ruleID         string
	occurrenceDate string
	status         string
	transactionID  sql.NullString
}

func scanScheduledOccurrenceRow(row rowScanner) (scheduledOccurrenceRow, error) {
	var oc scheduledOccurrenceRow
	err := row.Scan(&oc.id, &oc.ruleID, &oc.occurrenceDate, &oc.status, &oc.transactionID)
	return oc, err
}

func buildScheduledOccurrence(oc scheduledOccurrenceRow) (recurring.ScheduledOccurrence, error) {
	occurrenceDate, err := parseDate(oc.occurrenceDate)
	if err != nil {
		return recurring.ScheduledOccurrence{}, err
	}

	opts := []recurring.ScheduledOccurrenceOption{
		recurring.WithOccurrenceStatus(recurring.OccurrenceStatus(oc.status)),
	}
	if oc.transactionID.Valid {
		opts = append(opts, recurring.WithTransactionID(oc.transactionID.String))
	}
	return recurring.NewScheduledOccurrence(oc.id, oc.ruleID, occurrenceDate, opts...)
}
