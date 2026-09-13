package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/anirudhgray/bodger/internal/domain/budgeting"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// BudgetRepository implements ports.BudgetRepository over a *DB.
type BudgetRepository struct {
	db *DB
}

// NewBudgetRepository constructs a BudgetRepository backed by db.
func NewBudgetRepository(db *DB) *BudgetRepository {
	return &BudgetRepository{db: db}
}

var _ ports.BudgetRepository = (*BudgetRepository)(nil)

// Create implements ports.BudgetRepository.
func (r *BudgetRepository) Create(ctx context.Context, actorID string, budget budgeting.Budget) error {
	if err := requireActor(actorID, budget.UserID()); err != nil {
		return err
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	now := formatTime(r.db.clock.Now())

	if err := insertBudgetRow(ctx, tx, actorID, budget, now); err != nil {
		return err
	}
	if err := insertBudgetLines(ctx, tx, budget.ID(), budget.Lines()); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// Get implements ports.BudgetRepository.
func (r *BudgetRepository) Get(ctx context.Context, actorID, id string) (budgeting.Budget, error) {
	if err := requireActorID(actorID); err != nil {
		return budgeting.Budget{}, err
	}

	row := r.db.read.QueryRowContext(ctx, `
		SELECT id, user_id, name, period_type, currency, starts_on, archived_at
		FROM budgets
		WHERE id = ? AND user_id = ?
	`, id, actorID)

	br, err := scanBudgetRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return budgeting.Budget{}, errs.New(errs.NotFound).Explain("No budget with ID %q.", id).Field("id")
	}
	if err != nil {
		return budgeting.Budget{}, errs.New(errs.Internal).Wrap(err)
	}

	lines, err := loadBudgetLines(ctx, r.db.read, []string{id})
	if err != nil {
		return budgeting.Budget{}, errs.New(errs.Internal).Wrap(err)
	}

	budget, err := buildBudget(br, lines[id])
	if err != nil {
		return budgeting.Budget{}, errs.New(errs.Internal).Wrap(err)
	}
	return budget, nil
}

// List implements ports.BudgetRepository.
func (r *BudgetRepository) List(ctx context.Context, actorID string) ([]budgeting.Budget, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}

	rows, err := r.db.read.QueryContext(ctx, `
		SELECT id, user_id, name, period_type, currency, starts_on, archived_at
		FROM budgets
		WHERE user_id = ?
		ORDER BY created_at DESC, id DESC
	`, actorID)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var budgetRows []budgetRow
	var ids []string
	for rows.Next() {
		br, err := scanBudgetRow(rows)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		budgetRows = append(budgetRows, br)
		ids = append(ids, br.id)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}

	linesByBudget, err := loadBudgetLines(ctx, r.db.read, ids)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}

	budgets := make([]budgeting.Budget, 0, len(budgetRows))
	for _, br := range budgetRows {
		budget, err := buildBudget(br, linesByBudget[br.id])
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		budgets = append(budgets, budget)
	}
	return budgets, nil
}

// Update implements ports.BudgetRepository.
func (r *BudgetRepository) Update(ctx context.Context, actorID string, budget budgeting.Budget) error {
	if err := requireActor(actorID, budget.UserID()); err != nil {
		return err
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := formatTime(r.db.clock.Now())
	result, err := tx.ExecContext(ctx, `
		UPDATE budgets
		SET name = ?, period_type = ?, currency = ?, starts_on = ?, archived_at = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`,
		budget.Name(), string(budget.PeriodType()), budget.Currency(), formatDate(budget.StartsOn()),
		nullableArchivedAt(budget), now,
		budget.ID(), actorID,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't update budget %q.", budget.Name())
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No budget with ID %q.", budget.ID()).Field("id")
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM budget_lines WHERE budget_id = ?`, budget.ID()); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if err := insertBudgetLines(ctx, tx, budget.ID(), budget.Lines()); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// --- shared helpers ---

func insertBudgetRow(ctx context.Context, tx execer, actorID string, budget budgeting.Budget, now string) *errs.Error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO budgets (id, user_id, name, period_type, currency, starts_on, archived_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		budget.ID(), actorID, budget.Name(), string(budget.PeriodType()), budget.Currency(),
		formatDate(budget.StartsOn()), nullableArchivedAt(budget), now, now,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't create budget %q.", budget.Name())
	}
	return nil
}

func insertBudgetLines(ctx context.Context, tx execer, budgetID string, lines []budgeting.BudgetLine) *errs.Error {
	for _, l := range lines {
		rollover := 0
		if l.Rollover() {
			rollover = 1
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO budget_lines (id, budget_id, category_id, amount_minor, rollover)
			VALUES (?, ?, ?, ?, ?)
		`, l.ID(), budgetID, l.CategoryID(), l.AmountMinor(), rollover)
		if err != nil {
			return wrapWriteError(err).Explain("Couldn't save budget line %q.", l.ID())
		}
	}
	return nil
}

// budgetRow is the raw column set scanBudgetRow reads before buildBudget
// turns it into a domain budgeting.Budget (which needs its lines too,
// loaded separately) — the same two-step shape transactionRow/
// buildTransaction uses.
type budgetRow struct {
	id         string
	userID     string
	name       string
	periodType string
	currency   string
	startsOn   string
	archivedAt sql.NullString
}

func scanBudgetRow(row rowScanner) (budgetRow, error) {
	var br budgetRow
	err := row.Scan(&br.id, &br.userID, &br.name, &br.periodType, &br.currency, &br.startsOn, &br.archivedAt)
	return br, err
}

// loadBudgetLines loads every line for the given budget IDs in one query,
// keyed by budget_id — the same batching shape loadPostings uses for
// transactions.
func loadBudgetLines(ctx context.Context, q execer, budgetIDs []string) (map[string][]budgeting.BudgetLine, error) {
	result := make(map[string][]budgeting.BudgetLine, len(budgetIDs))
	if len(budgetIDs) == 0 {
		return result, nil
	}

	placeholders := make([]byte, 0, len(budgetIDs)*2)
	args := make([]any, len(budgetIDs))
	for i, id := range budgetIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, budget_id, category_id, amount_minor, rollover
		FROM budget_lines
		WHERE budget_id IN (%s)
		ORDER BY budget_id, id
	`, string(placeholders))

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			id, budgetID, categoryID string
			amountMinor              int64
			rollover                 int
		)
		if err := rows.Scan(&id, &budgetID, &categoryID, &amountMinor, &rollover); err != nil {
			return nil, err
		}
		l, err := budgeting.NewBudgetLine(id, budgetID, categoryID, amountMinor, rollover != 0)
		if err != nil {
			return nil, err
		}
		result[budgetID] = append(result[budgetID], l)
	}
	return result, rows.Err()
}

// buildBudget reconstructs a domain budgeting.Budget from its raw row and
// lines — the same two-step reconstruction shape buildTransaction uses for
// transactions and postings.
func buildBudget(br budgetRow, lines []budgeting.BudgetLine) (budgeting.Budget, error) {
	startsOn, err := parseDate(br.startsOn)
	if err != nil {
		return budgeting.Budget{}, err
	}

	var opts []budgeting.BudgetOption
	if br.archivedAt.Valid {
		d, err := parseDate(br.archivedAt.String)
		if err != nil {
			return budgeting.Budget{}, err
		}
		opts = append(opts, budgeting.WithArchivedAt(d))
	}

	return budgeting.NewBudget(br.id, br.userID, br.name, budgeting.PeriodType(br.periodType), br.currency, startsOn, lines, opts...)
}

func nullableArchivedAt(budget budgeting.Budget) sql.NullString {
	d, ok := budget.ArchivedAt()
	return nullableDate(d, ok)
}
