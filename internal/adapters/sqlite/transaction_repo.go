package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// TransactionRepository implements ports.TransactionRepository over a *DB.
type TransactionRepository struct {
	db *DB
}

// NewTransactionRepository constructs a TransactionRepository backed by db.
func NewTransactionRepository(db *DB) *TransactionRepository {
	return &TransactionRepository{db: db}
}

var _ ports.TransactionRepository = (*TransactionRepository)(nil)

// Create implements ports.TransactionRepository.
func (r *TransactionRepository) Create(ctx context.Context, actorID string, txn ledger.Transaction, tags []ledger.Tag) error {
	if err := requireActor(actorID, txn.UserID()); err != nil {
		return err
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	now := formatTime(r.db.clock.Now())

	if err := insertTransactionRow(ctx, tx, actorID, txn, now); err != nil {
		return err
	}
	if err := insertPostings(ctx, tx, txn.ID(), txn.Postings()); err != nil {
		return err
	}
	if err := setTransactionTags(ctx, tx, actorID, txn.ID(), tags, now); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// Get implements ports.TransactionRepository.
func (r *TransactionRepository) Get(ctx context.Context, actorID, id string) (ledger.Transaction, []ledger.Tag, error) {
	if err := requireActorID(actorID); err != nil {
		return ledger.Transaction{}, nil, err
	}

	row := r.db.read.QueryRowContext(ctx, `
		SELECT id, user_id, kind, booked_date, posted_date, description, notes,
		       import_record_id, external_id, related_transaction_id, created_at
		FROM transactions
		WHERE id = ? AND user_id = ? AND deleted_at IS NULL
	`, id, actorID)

	txnRow, err := scanTransactionRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.Transaction{}, nil, errs.New(errs.NotFound).Explain("No transaction with ID %q.", id).Field("id")
	}
	if err != nil {
		return ledger.Transaction{}, nil, errs.New(errs.Internal).Wrap(err)
	}

	postings, err := loadPostings(ctx, r.db.read, []string{id})
	if err != nil {
		return ledger.Transaction{}, nil, errs.New(errs.Internal).Wrap(err)
	}

	txn, err := buildTransaction(txnRow, postings[id])
	if err != nil {
		return ledger.Transaction{}, nil, errs.New(errs.Internal).Wrap(err)
	}

	tags, err := loadTagsForTransaction(ctx, r.db.read, actorID, id)
	if err != nil {
		return ledger.Transaction{}, nil, errs.New(errs.Internal).Wrap(err)
	}

	return txn, tags, nil
}

// List implements ports.TransactionRepository.
func (r *TransactionRepository) List(ctx context.Context, actorID string, filter ports.TransactionFilter) ([]ledger.Transaction, error) {
	if err := requireActorID(actorID); err != nil {
		return nil, err
	}

	// filter.CategoryID resolves against a recursive CTE walking the
	// category tree downward (children of children, ...) from the given
	// root — the same recursive shape checkNoCycle uses to walk upward,
	// but in the opposite direction: subtree inclusion (ADR-0009), not
	// cycle detection. Its placeholders come first in the finished query
	// text, so its args are prepended to args below, ahead of every WHERE
	// placeholder.
	var cte string
	var cteArgs []any
	if filter.CategoryID != "" {
		cte = `
			WITH RECURSIVE category_subtree(id) AS (
				SELECT id FROM categories WHERE id = ? AND user_id = ?
				UNION ALL
				SELECT c.id FROM categories c JOIN category_subtree cs ON c.parent_id = cs.id
			)
		`
		cteArgs = []any{filter.CategoryID, actorID}
	}

	query := cte + `
		SELECT DISTINCT t.id, t.user_id, t.kind, t.booked_date, t.posted_date, t.description, t.notes,
		       t.import_record_id, t.external_id, t.related_transaction_id, t.created_at
		FROM transactions t
	`

	where := []string{"t.user_id = ?", "t.deleted_at IS NULL"}
	whereArgs := []any{actorID}

	if filter.AccountID != "" || filter.CategoryID != "" {
		query += " JOIN postings p ON p.transaction_id = t.id"
	}
	if filter.AccountID != "" {
		where = append(where, "p.account_id = ?")
		whereArgs = append(whereArgs, filter.AccountID)
	}
	if filter.CategoryID != "" {
		where = append(where, "p.category_id IN (SELECT id FROM category_subtree)")
	}
	if filter.Kind != "" {
		where = append(where, "t.kind = ?")
		whereArgs = append(whereArgs, string(filter.Kind))
	}
	if filter.FromDate != nil {
		where = append(where, "t.booked_date >= ?")
		whereArgs = append(whereArgs, formatDate(*filter.FromDate))
	}
	if filter.ToDate != nil {
		where = append(where, "t.booked_date <= ?")
		whereArgs = append(whereArgs, formatDate(*filter.ToDate))
	}

	query += " WHERE " + strings.Join(where, " AND ")

	// The fully-specified sort ADR-0009 requires: a non-deterministic
	// tiebreak would make offset pagination (and any test asserting exact
	// order) unreliable.
	query += " ORDER BY t.booked_date DESC, t.created_at DESC, t.id DESC"

	var limitArgs []any
	switch {
	case filter.Limit > 0 && filter.Offset > 0:
		query += " LIMIT ? OFFSET ?"
		limitArgs = []any{filter.Limit, filter.Offset}
	case filter.Limit > 0:
		query += " LIMIT ?"
		limitArgs = []any{filter.Limit}
	case filter.Offset > 0:
		// SQLite-specific: LIMIT -1 means "no limit", which is what lets
		// OFFSET apply on its own when the caller wants a page of
		// everything past a point without also capping page size.
		query += " LIMIT -1 OFFSET ?"
		limitArgs = []any{filter.Offset}
	}

	args := make([]any, 0, len(cteArgs)+len(whereArgs)+len(limitArgs))
	args = append(args, cteArgs...)
	args = append(args, whereArgs...)
	args = append(args, limitArgs...)

	rows, err := r.db.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	defer func() { _ = rows.Close() }()

	var txnRows []transactionRow
	var ids []string
	for rows.Next() {
		txnRow, err := scanTransactionRow(rows)
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		txnRows = append(txnRows, txnRow)
		ids = append(ids, txnRow.id)
	}
	if err := rows.Err(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}

	postingsByTxn, err := loadPostings(ctx, r.db.read, ids)
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}

	// txnRows is already in the query's final sort order; preserve it
	// rather than iterating postingsByTxn (a map, unordered) or re-deriving
	// order from ids.
	txns := make([]ledger.Transaction, 0, len(txnRows))
	for _, tr := range txnRows {
		txn, err := buildTransaction(tr, postingsByTxn[tr.id])
		if err != nil {
			return nil, errs.New(errs.Internal).Wrap(err)
		}
		txns = append(txns, txn)
	}
	return txns, nil
}

// Update implements ports.TransactionRepository.
func (r *TransactionRepository) Update(ctx context.Context, actorID string, txn ledger.Transaction, tags []ledger.Tag) error {
	if err := requireActor(actorID, txn.UserID()); err != nil {
		return err
	}

	tx, err := r.db.write.BeginTx(ctx, nil)
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := formatTime(r.db.clock.Now())

	// Deliberately a fresh variable, not "err": loadCurrentForRevision
	// returns a *errs.Error, and reusing the outer "err error" here would
	// box a nil *errs.Error into a non-nil error interface — the classic
	// Go typed-nil trap — turning every successful call into a spurious
	// error.
	previous, prevTags, revErr := loadCurrentForRevision(ctx, tx, actorID, txn.ID())
	if revErr != nil {
		return revErr
	}

	snapshot, err := json.Marshal(revisionSnapshot(previous, prevTags))
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO transaction_revisions (transaction_id, user_id, previous_state, created_at)
		VALUES (?, ?, ?, ?)
	`, txn.ID(), actorID, string(snapshot), now); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE transactions
		SET kind = ?, booked_date = ?, posted_date = ?, description = ?, notes = ?,
		    deleted_at = ?, import_record_id = ?, external_id = ?, related_transaction_id = ?, updated_at = ?
		WHERE id = ? AND user_id = ?
	`,
		string(txn.Kind()), formatDate(txn.BookedDate()), nullablePostedDate(txn), txn.Description(), txn.Notes(),
		nullableDeletedAt(txn), nullableImportRecordID(txn), nullableExternalID(txn), nullableRelatedTransactionID(txn), now,
		txn.ID(), actorID,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't update transaction %q.", txn.Description())
	}
	n, err := result.RowsAffected()
	if err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if n == 0 {
		return errs.New(errs.NotFound).Explain("No transaction with ID %q.", txn.ID()).Field("id")
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM postings WHERE transaction_id = ?`, txn.ID()); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if err := insertPostings(ctx, tx, txn.ID(), txn.Postings()); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM transaction_tags WHERE transaction_id = ?`, txn.ID()); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	if err := setTransactionTags(ctx, tx, actorID, txn.ID(), tags, now); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return errs.New(errs.Internal).Wrap(err)
	}
	return nil
}

// --- shared helpers ---

// execer is the subset of *sql.DB and *sql.Tx this package's write helpers
// need, so they work identically inside Create's and Update's transactions.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func insertTransactionRow(ctx context.Context, tx execer, actorID string, txn ledger.Transaction, now string) *errs.Error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO transactions (id, user_id, kind, booked_date, posted_date, description, notes,
		                          deleted_at, import_record_id, external_id, related_transaction_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		txn.ID(), actorID, string(txn.Kind()), formatDate(txn.BookedDate()), nullablePostedDate(txn),
		txn.Description(), txn.Notes(), nullableDeletedAt(txn), nullableImportRecordID(txn),
		nullableExternalID(txn), nullableRelatedTransactionID(txn), now, now,
	)
	if err != nil {
		return wrapWriteError(err).Explain("Couldn't create transaction %q.", txn.Description())
	}
	return nil
}

func insertPostings(ctx context.Context, tx execer, transactionID string, postings []ledger.Posting) *errs.Error {
	for _, p := range postings {
		categoryID, hasCategory := p.CategoryID()
		_, err := tx.ExecContext(ctx, `
			INSERT INTO postings (id, transaction_id, account_id, amount_minor, currency, category_id, sort_order)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, p.ID(), transactionID, p.AccountID(), p.Amount().AmountMinor(), p.Currency(),
			nullableString(categoryID, hasCategory), p.SortOrder())
		if err != nil {
			return wrapWriteError(err).Explain("Couldn't save posting %q.", p.ID())
		}
	}
	return nil
}

func setTransactionTags(ctx context.Context, tx execer, actorID, transactionID string, tags []ledger.Tag, now string) *errs.Error {
	for _, t := range tags {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tags (user_id, value, created_at)
			VALUES (?, ?, ?)
			ON CONFLICT (user_id, value) DO NOTHING
		`, actorID, t.String(), now); err != nil {
			return errs.New(errs.Internal).Wrap(err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO transaction_tags (transaction_id, user_id, tag_value)
			VALUES (?, ?, ?)
		`, transactionID, actorID, t.String()); err != nil {
			return wrapWriteError(err).Explain("Couldn't tag transaction with %q.", t.String())
		}
	}
	return nil
}

func loadTagsForTransaction(ctx context.Context, q execer, actorID, transactionID string) ([]ledger.Tag, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT tag_value FROM transaction_tags
		WHERE transaction_id = ? AND user_id = ?
		ORDER BY tag_value
	`, transactionID, actorID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tags []ledger.Tag
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		t, err := ledger.NewTag(value)
		if err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// transactionRow is the raw column set scanTransactionRow reads before
// buildTransaction turns it into a domain ledger.Transaction (which needs
// its postings too, loaded separately).
type transactionRow struct {
	id                   string
	userID               string
	kind                 string
	bookedDate           string
	postedDate           sql.NullString
	description          string
	notes                string
	importRecordID       sql.NullString
	externalID           sql.NullString
	relatedTransactionID sql.NullString
	// createdAt is scanned only to appear in List's SQL-level ORDER BY
	// (booked_date DESC, created_at DESC, id DESC — ADR-0009); it never
	// reaches the domain Transaction, which has no CreatedAt accessor
	// (created_at is audit-only, data-model.md §9).
	createdAt string
}

func scanTransactionRow(row rowScanner) (transactionRow, error) {
	var tr transactionRow
	err := row.Scan(&tr.id, &tr.userID, &tr.kind, &tr.bookedDate, &tr.postedDate, &tr.description, &tr.notes,
		&tr.importRecordID, &tr.externalID, &tr.relatedTransactionID, &tr.createdAt)
	return tr, err
}

// loadPostings loads every posting for the given transaction IDs in one
// query, keyed by transaction_id and ordered by sort_order — the shape
// List's N-transaction fetch and Get's single-transaction fetch both need.
func loadPostings(ctx context.Context, q execer, transactionIDs []string) (map[string][]ledger.Posting, error) {
	result := make(map[string][]ledger.Posting, len(transactionIDs))
	if len(transactionIDs) == 0 {
		return result, nil
	}

	placeholders := make([]byte, 0, len(transactionIDs)*2)
	args := make([]any, len(transactionIDs))
	for i, id := range transactionIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, transaction_id, account_id, amount_minor, currency, category_id, sort_order
		FROM postings
		WHERE transaction_id IN (%s)
		ORDER BY transaction_id, sort_order
	`, string(placeholders))

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			id, transactionID, accountID, currency string
			amountMinor                            int64
			categoryID                             sql.NullString
			sortOrder                              int
		)
		if err := rows.Scan(&id, &transactionID, &accountID, &amountMinor, &currency, &categoryID, &sortOrder); err != nil {
			return nil, err
		}
		m, err := money.NewMoney(amountMinor, currency)
		if err != nil {
			return nil, err
		}
		var categoryIDPtr *string
		if categoryID.Valid {
			v := categoryID.String
			categoryIDPtr = &v
		}
		p, err := ledger.NewPosting(id, accountID, m, categoryIDPtr, sortOrder)
		if err != nil {
			return nil, err
		}
		result[transactionID] = append(result[transactionID], p)
	}
	return result, rows.Err()
}

// buildTransaction reconstructs a domain ledger.Transaction from its raw
// row and postings, dispatching to the right constructor for its kind —
// the only way any of the three exist, so reconstruction has to go through
// the same validation construction did.
func buildTransaction(tr transactionRow, postings []ledger.Posting) (ledger.Transaction, error) {
	bookedDate, err := parseDate(tr.bookedDate)
	if err != nil {
		return ledger.Transaction{}, err
	}

	var opts []ledger.TransactionOption
	if tr.notes != "" {
		opts = append(opts, ledger.WithNotes(tr.notes))
	}
	if tr.postedDate.Valid {
		d, err := parseDate(tr.postedDate.String)
		if err != nil {
			return ledger.Transaction{}, err
		}
		opts = append(opts, ledger.WithPostedDate(d))
	}
	if tr.relatedTransactionID.Valid {
		opts = append(opts, ledger.WithRelatedTransaction(tr.relatedTransactionID.String))
	}
	if tr.importRecordID.Valid || tr.externalID.Valid {
		opts = append(opts, ledger.WithImportProvenance(tr.importRecordID.String, tr.externalID.String))
	}

	var out ledger.Transaction
	switch ledger.TransactionKind(tr.kind) {
	case ledger.TransactionKindOutflow:
		out, err = ledger.NewOutflow(tr.id, tr.userID, bookedDate, tr.description, postings, opts...)
	case ledger.TransactionKindInflow:
		out, err = ledger.NewInflow(tr.id, tr.userID, bookedDate, tr.description, postings, opts...)
	case ledger.TransactionKindTransfer:
		// The implied rate NewTransfer now also returns isn't persisted
		// yet (issue #128/#133 wire fx_rate_used/fx_rate_source); this is
		// a read-path reconstruction of an already-stored transaction, so
		// there's nothing to do with it here but discard it.
		out, _, err = ledger.NewTransfer(tr.id, tr.userID, bookedDate, tr.description, postings, opts...)
	default:
		return ledger.Transaction{}, fmt.Errorf("sqlite: unknown stored transaction kind %q", tr.kind)
	}
	return out, err
}

// loadCurrentForRevision fetches the transaction and tags currently stored
// for id (not yet overwritten by the caller's Update), for the audit
// snapshot data-model.md §7 requires before an edit is applied. It returns
// NotFound if no such non-deleted transaction exists for actorID.
func loadCurrentForRevision(ctx context.Context, tx execer, actorID, id string) (ledger.Transaction, []ledger.Tag, *errs.Error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, user_id, kind, booked_date, posted_date, description, notes,
		       import_record_id, external_id, related_transaction_id, created_at
		FROM transactions
		WHERE id = ? AND user_id = ? AND deleted_at IS NULL
	`, id, actorID)

	tr, err := scanTransactionRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ledger.Transaction{}, nil, errs.New(errs.NotFound).Explain("No transaction with ID %q.", id).Field("id")
	}
	if err != nil {
		return ledger.Transaction{}, nil, errs.New(errs.Internal).Wrap(err)
	}

	postings, err := loadPostings(ctx, tx, []string{id})
	if err != nil {
		return ledger.Transaction{}, nil, errs.New(errs.Internal).Wrap(err)
	}
	txn, err := buildTransaction(tr, postings[id])
	if err != nil {
		return ledger.Transaction{}, nil, errs.New(errs.Internal).Wrap(err)
	}

	tags, err := loadTagsForTransaction(ctx, tx, actorID, id)
	if err != nil {
		return ledger.Transaction{}, nil, errs.New(errs.Internal).Wrap(err)
	}
	return txn, tags, nil
}

// revisionPosting and revisionState are the JSON shape written to
// transaction_revisions.previous_state — a plain, untyped snapshot for a
// human reading "what did I change", never read back into a domain type
// (data-model.md §7: "Analytics never read transaction_revision").
type revisionPosting struct {
	AccountID   string `json:"account_id"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	CategoryID  string `json:"category_id,omitempty"`
}

type revisionState struct {
	Kind        string            `json:"kind"`
	BookedDate  string            `json:"booked_date"`
	Description string            `json:"description"`
	Notes       string            `json:"notes,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Postings    []revisionPosting `json:"postings"`
}

func revisionSnapshot(txn ledger.Transaction, tags []ledger.Tag) revisionState {
	postings := make([]revisionPosting, 0, len(txn.Postings()))
	for _, p := range txn.Postings() {
		categoryID, _ := p.CategoryID()
		postings = append(postings, revisionPosting{
			AccountID:   p.AccountID(),
			AmountMinor: p.Amount().AmountMinor(),
			Currency:    p.Currency(),
			CategoryID:  categoryID,
		})
	}
	tagValues := make([]string, 0, len(tags))
	for _, t := range tags {
		tagValues = append(tagValues, t.String())
	}
	return revisionState{
		Kind:        string(txn.Kind()),
		BookedDate:  txn.BookedDate().String(),
		Description: txn.Description(),
		Notes:       txn.Notes(),
		Tags:        tagValues,
		Postings:    postings,
	}
}

func nullablePostedDate(txn ledger.Transaction) sql.NullString {
	d, ok := txn.PostedDate()
	return nullableDate(d, ok)
}

func nullableDeletedAt(txn ledger.Transaction) sql.NullString {
	t, ok := txn.DeletedAt()
	if !ok {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(t), Valid: true}
}

func nullableImportRecordID(txn ledger.Transaction) sql.NullString {
	v, ok := txn.ImportRecordID()
	return nullableString(v, ok)
}

func nullableExternalID(txn ledger.Transaction) sql.NullString {
	v, ok := txn.ExternalID()
	return nullableString(v, ok)
}

func nullableRelatedTransactionID(txn ledger.Transaction) sql.NullString {
	v, ok := txn.RelatedTransactionID()
	return nullableString(v, ok)
}
