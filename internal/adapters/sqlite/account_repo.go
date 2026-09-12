package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// AccountRepository implements ports.AccountRepository over a *DB.
type AccountRepository struct {
	db *DB
}

// NewAccountRepository constructs an AccountRepository backed by db.
func NewAccountRepository(db *DB) *AccountRepository {
	return &AccountRepository{db: db}
}

var _ ports.AccountRepository = (*AccountRepository)(nil)

// Create implements ports.AccountRepository.
func (r *AccountRepository) Create(ctx context.Context, actorID string, account ledger.Account) error {
	if err := requireActor(actorID, account.UserID()); err != nil {
		return err
	}

	now := formatTime(r.db.clock.Now())
	if err := insertAccountRow(ctx, r.db.write, actorID, account, now); err != nil {
		return err
	}
	return nil
}

// insertAccountRow writes one accounts row through tx. It's shared by
// AccountRepository.Create (a single row, its own implicit transaction) and
// SnapshotRepository.Replace (issue #226: every restored account, inside
// the one transaction that replaces an actor's whole ledger) — the same
// INSERT either way, so the two write paths can't drift on which columns
// get written.
func insertAccountRow(ctx context.Context, tx execer, actorID string, account ledger.Account, now string) *errs.Error {
	obDate, hasOBDate := account.OpeningBalanceDate()
	institution, hasInstitution := account.Institution()
	archivedAt, isArchived := account.ArchivedAt()

	_, err := tx.ExecContext(ctx, `
		INSERT INTO accounts (id, user_id, name, kind, currency, institution, opening_balance_minor, opening_balance_date, archived_at, sort_order, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		account.ID(), actorID, account.Name(), string(account.Kind()), account.Currency(),
		nullableString(institution, hasInstitution),
		account.OpeningBalance().AmountMinor(), nullableDate(obDate, hasOBDate),
		nullableDate(archivedAt, isArchived), account.SortOrder(),
		now, now,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't create account %q.", account.Name())
	}
	return nil
}

// Get implements ports.AccountRepository.
func (r *AccountRepository) Get(ctx context.Context, actorID, id string) (ledger.Account, error) {
	if err := requireActorID(actorID); err != nil {
		return ledger.Account{}, err
	}

	row := r.db.read.QueryRowContext(ctx, `
		SELECT id, user_id, name, kind, currency, institution, opening_balance_minor, opening_balance_date, archived_at, sort_order
		FROM accounts
		WHERE id = ? AND user_id = ?
	`, id, actorID)

	account, err := scanAccount(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.Account{}, errs.New(errs.NotFound).Explain("No account with ID %q.", id).Field("id")
	}
	if err != nil {
		return ledger.Account{}, errs.New(errs.Internal).Wrap(err)
	}
	return account, nil
}

// List implements ports.AccountRepository.
func (r *AccountRepository) List(ctx context.Context, actorID string) ([]ledger.Account, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}

	rows, err := r.db.read.QueryContext(ctx, `
		SELECT id, user_id, name, kind, currency, institution, opening_balance_minor, opening_balance_date, archived_at, sort_order
		FROM accounts
		WHERE user_id = ?
		ORDER BY name
	`, actorID)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var accounts []ledger.Account
	for rows.Next() {
		account, err := scanAccount(rows)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	return accounts, nil
}

// Update implements ports.AccountRepository.
func (r *AccountRepository) Update(ctx context.Context, actorID string, account ledger.Account) error {
	if err := requireActor(actorID, account.UserID()); err != nil {
		return err
	}

	now := formatTime(r.db.clock.Now())
	obDate, hasOBDate := account.OpeningBalanceDate()
	institution, hasInstitution := account.Institution()
	archivedAt, isArchived := account.ArchivedAt()

	result, err := r.db.write.ExecContext(ctx, `
		UPDATE accounts
		SET name = ?, kind = ?, currency = ?, institution = ?, opening_balance_minor = ?, opening_balance_date = ?, archived_at = ?, sort_order = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`,
		account.Name(), string(account.Kind()), account.Currency(), nullableString(institution, hasInstitution),
		account.OpeningBalance().AmountMinor(), nullableDate(obDate, hasOBDate),
		nullableDate(archivedAt, isArchived), account.SortOrder(), now,
		account.ID(), actorID,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't update account %q.", account.Name())
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No account with ID %q.", account.ID()).Field("id")
	}
	return nil
}

// rowScanner is the subset of *sql.Row and *sql.Rows this package scans
// through — a small seam so scan helpers work with either.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanAccount(row rowScanner) (ledger.Account, error) {
	var (
		id, userID, name, kind, currency string
		institutionCol                   sql.NullString
		amountMinor                      int64
		obDateCol                        sql.NullString
		archivedAtCol                    sql.NullString
		sortOrder                        int
	)
	if err := row.Scan(&id, &userID, &name, &kind, &currency, &institutionCol, &amountMinor, &obDateCol, &archivedAtCol, &sortOrder); err != nil {
		return ledger.Account{}, err
	}

	m, err := money.NewMoney(amountMinor, currency)
	if err != nil {
		return ledger.Account{}, err
	}

	var obDatePtr *domain.Date
	if obDateCol.Valid {
		d, err := parseDate(obDateCol.String)
		if err != nil {
			return ledger.Account{}, err
		}
		obDatePtr = &d
	}

	var institutionPtr *string
	if institutionCol.Valid {
		v := institutionCol.String
		institutionPtr = &v
	}

	var archivedAtPtr *domain.Date
	if archivedAtCol.Valid {
		d, err := parseDate(archivedAtCol.String)
		if err != nil {
			return ledger.Account{}, err
		}
		archivedAtPtr = &d
	}

	return ledger.NewAccount(id, userID, name, ledger.AccountKind(kind), m, obDatePtr, institutionPtr, sortOrder, archivedAtPtr)
}
