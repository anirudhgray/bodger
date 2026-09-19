package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/recurring"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// RecurringRuleRepository implements ports.RecurringRuleRepository over a
// *DB. The schedule is stored as typed columns rather than an RRULE string
// (ADR-0014), so scheduleColumns/buildSchedule below are the two halves of
// that mapping.
type RecurringRuleRepository struct {
	db *DB
}

// NewRecurringRuleRepository constructs a RecurringRuleRepository backed
// by db.
func NewRecurringRuleRepository(db *DB) *RecurringRuleRepository {
	return &RecurringRuleRepository{db: db}
}

var _ ports.RecurringRuleRepository = (*RecurringRuleRepository)(nil)

const recurringRuleColumns = `
	id, user_id, account_id, category_id, amount_minor, description,
	frequency, interval_count, weekday, day_of_month, month_of_year,
	starts_on, ends_on, archived_at
`

// Create implements ports.RecurringRuleRepository.
func (r *RecurringRuleRepository) Create(ctx context.Context, actorID string, rule recurring.RecurringRule) error {
	if err := requireActor(actorID, rule.UserID()); err != nil {
		return err
	}
	now := formatTime(r.db.clock.Now())
	if err := insertRecurringRuleRow(ctx, r.db.write, actorID, rule, now); err != nil {
		return err
	}
	return nil
}

// insertRecurringRuleRow writes rule's INSERT against tx, so Create and
// SnapshotRepository.Replace (issue #283) run the identical statement
// inside their own respective transactions rather than duplicating this
// SQL — the same shared-helper shape insertBudgetRow already gives
// BudgetRepository.Create/SnapshotRepository.Replace.
func insertRecurringRuleRow(ctx context.Context, tx execer, actorID string, rule recurring.RecurringRule, now string) *errs.Error {
	cols := scheduleColumns(rule.Schedule())

	_, err := tx.ExecContext(ctx, `
		INSERT INTO recurring_rules (id, user_id, account_id, category_id, amount_minor, description,
		                             frequency, interval_count, weekday, day_of_month, month_of_year,
		                             starts_on, ends_on, archived_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rule.ID(), actorID, rule.AccountID(), rule.CategoryID(), rule.AmountMinor(), rule.Description(),
		cols.frequency, cols.interval, cols.weekday, cols.dayOfMonth, cols.monthOfYear,
		formatDate(rule.StartsOn()), nullableDate(rule.EndsOn()), nullableDate(rule.ArchivedAt()), now, now,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't create recurring rule %q.", rule.Description())
	}
	return nil
}

// Get implements ports.RecurringRuleRepository.
func (r *RecurringRuleRepository) Get(ctx context.Context, actorID, id string) (recurring.RecurringRule, error) {
	if err := requireActorID(actorID); err != nil {
		return recurring.RecurringRule{}, err
	}

	row := r.db.read.QueryRowContext(ctx, `
		SELECT `+recurringRuleColumns+`
		FROM recurring_rules
		WHERE id = ? AND user_id = ?
	`, id, actorID)

	rr, err := scanRecurringRuleRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return recurring.RecurringRule{}, errs.New(errs.NotFound).Explain("No recurring rule with ID %q.", id).Field("id")
	}
	if err != nil {
		return recurring.RecurringRule{}, errs.New(errs.Internal).Wrap(err)
	}

	rule, err := buildRecurringRule(rr)
	if err != nil {
		return recurring.RecurringRule{}, errs.New(errs.Internal).Wrap(err)
	}
	return rule, nil
}

// List implements ports.RecurringRuleRepository.
func (r *RecurringRuleRepository) List(ctx context.Context, actorID string) ([]recurring.RecurringRule, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}

	rows, err := r.db.read.QueryContext(ctx, `
		SELECT `+recurringRuleColumns+`
		FROM recurring_rules
		WHERE user_id = ?
		ORDER BY created_at DESC, id DESC
	`, actorID)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var rules []recurring.RecurringRule
	for rows.Next() {
		rr, err := scanRecurringRuleRow(rows)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		rule, err := buildRecurringRule(rr)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	return rules, nil
}

// Update implements ports.RecurringRuleRepository.
func (r *RecurringRuleRepository) Update(ctx context.Context, actorID string, rule recurring.RecurringRule) error {
	if err := requireActor(actorID, rule.UserID()); err != nil {
		return err
	}

	now := formatTime(r.db.clock.Now())
	cols := scheduleColumns(rule.Schedule())

	result, err := r.db.write.ExecContext(ctx, `
		UPDATE recurring_rules
		SET account_id = ?, category_id = ?, amount_minor = ?, description = ?,
		    frequency = ?, interval_count = ?, weekday = ?, day_of_month = ?, month_of_year = ?,
		    starts_on = ?, ends_on = ?, archived_at = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`,
		rule.AccountID(), rule.CategoryID(), rule.AmountMinor(), rule.Description(),
		cols.frequency, cols.interval, cols.weekday, cols.dayOfMonth, cols.monthOfYear,
		formatDate(rule.StartsOn()), nullableDate(rule.EndsOn()), nullableDate(rule.ArchivedAt()), now,
		rule.ID(), actorID,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't update recurring rule %q.", rule.Description())
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No recurring rule with ID %q.", rule.ID()).Field("id")
	}
	return nil
}

// --- schedule column mapping ---

// recurringScheduleColumns is the typed-column form of a
// recurring.Schedule. Exactly the columns the declared frequency needs are
// non-NULL, matching the CHECK constraint in migration 00016.
type recurringScheduleColumns struct {
	frequency   string
	interval    int
	weekday     sql.NullInt64
	dayOfMonth  sql.NullInt64
	monthOfYear sql.NullInt64
}

func scheduleColumns(s recurring.Schedule) recurringScheduleColumns {
	cols := recurringScheduleColumns{frequency: string(s.Frequency()), interval: s.Interval()}
	if wd, ok := s.Weekday(); ok {
		cols.weekday = sql.NullInt64{Int64: int64(wd), Valid: true}
	}
	if day, ok := s.DayOfMonth(); ok {
		cols.dayOfMonth = sql.NullInt64{Int64: int64(day), Valid: true}
	}
	if month, ok := s.Month(); ok {
		cols.monthOfYear = sql.NullInt64{Int64: int64(month), Valid: true}
	}
	return cols
}

// buildSchedule is scheduleColumns' inverse, handing the stored values
// back to recurring.NewSchedule rather than reconstructing a Schedule
// field by field — so a row that somehow escaped the schema's own CHECK
// still can't produce a schedule the domain layer would have rejected.
func buildSchedule(cols recurringScheduleColumns) (recurring.Schedule, error) {
	return recurring.NewSchedule(
		recurring.Frequency(cols.frequency),
		cols.interval,
		time.Weekday(cols.weekday.Int64),
		time.Month(cols.monthOfYear.Int64),
		int(cols.dayOfMonth.Int64),
	)
}

// --- row scanning ---

// recurringRuleRow is the raw column set scanRecurringRuleRow reads before
// buildRecurringRule turns it into a domain recurring.RecurringRule — the
// same two-step shape budgetRow/buildBudget uses.
type recurringRuleRow struct {
	id          string
	userID      string
	accountID   string
	categoryID  string
	amountMinor int64
	description string
	schedule    recurringScheduleColumns
	startsOn    string
	endsOn      sql.NullString
	archivedAt  sql.NullString
}

func scanRecurringRuleRow(row rowScanner) (recurringRuleRow, error) {
	var rr recurringRuleRow
	err := row.Scan(
		&rr.id, &rr.userID, &rr.accountID, &rr.categoryID, &rr.amountMinor, &rr.description,
		&rr.schedule.frequency, &rr.schedule.interval, &rr.schedule.weekday, &rr.schedule.dayOfMonth, &rr.schedule.monthOfYear,
		&rr.startsOn, &rr.endsOn, &rr.archivedAt,
	)
	return rr, err
}

func buildRecurringRule(rr recurringRuleRow) (recurring.RecurringRule, error) {
	schedule, err := buildSchedule(rr.schedule)
	if err != nil {
		return recurring.RecurringRule{}, err
	}
	startsOn, err := parseDate(rr.startsOn)
	if err != nil {
		return recurring.RecurringRule{}, err
	}

	var opts []recurring.RecurringRuleOption
	if rr.endsOn.Valid {
		d, err := parseDate(rr.endsOn.String)
		if err != nil {
			return recurring.RecurringRule{}, err
		}
		opts = append(opts, recurring.WithEndsOn(d))
	}
	if rr.archivedAt.Valid {
		d, err := parseDate(rr.archivedAt.String)
		if err != nil {
			return recurring.RecurringRule{}, err
		}
		opts = append(opts, recurring.WithArchivedAt(d))
	}

	return recurring.NewRecurringRule(rr.id, rr.userID, rr.accountID, rr.categoryID,
		rr.amountMinor, rr.description, schedule, startsOn, opts...)
}
