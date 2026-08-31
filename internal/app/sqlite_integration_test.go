package app_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/adapters/sqlite"
	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	"github.com/anirudhgray/bodger/internal/ports"
)

// newSQLiteTestService wires an app.Service to the real SQLite adapter
// (internal/adapters/sqlite) against a fresh, fully-migrated temp-file
// database, rather than the in-memory fakes memrepo_test.go provides.
//
// Most of issue #6's use-case tests are correctly served by the in-memory
// fakes (issue #6's "done when": "use-case tests run against in-memory
// repositories"). A few of the issue's own "done when" assertions are
// specifically facts about persistence — a revision row actually exists in
// transaction_revisions, a soft-deleted row is actually still present in
// the transactions table — that an in-memory fake could only ever fake
// agreeing with, which would prove nothing. Those specific assertions use
// this instead, plus the verify *sql.DB it also returns: a second,
// independent connection to the same file opened with plain
// database/sql (the "sqlite" driver is already registered process-wide by
// importing internal/adapters/sqlite), used only to read tables no
// ports.TransactionRepository method exposes — never to write, and never
// something internal/app's own production code does (architecture.md §2:
// "The application layer never sees a *sql.DB... it depends only on
// internal/ports").
func newSQLiteTestService(t *testing.T, frozenAt time.Time, tz string) (svc *app.Service, clk *clock.Frozen, verify *sql.DB) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "bodger.db")
	clk = clock.NewFrozen(frozenAt)

	db, err := sqlite.Open(clk, path)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("db.Close: %v", err)
		}
	})
	if err := db.MigrateUp(context.Background()); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}

	verify, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open(verify): %v", err)
	}
	t.Cleanup(func() {
		if err := verify.Close(); err != nil {
			t.Errorf("verify.Close: %v", err)
		}
	})

	cfg := config.Defaults
	cfg.UserTimezone = tz

	svc, err = app.NewService(
		clk, cfg, idgen.New(),
		sqlite.NewAccountRepository(db), sqlite.NewCategoryRepository(db),
		sqlite.NewTransactionRepository(db), sqlite.NewTagRepository(db),
	)
	if err != nil {
		t.Fatalf("app.NewService: %v", err)
	}
	return svc, clk, verify
}

// sqliteActorID is the seeded M1 user (ports.SeededUserID) every
// sqlite-backed integration test in this package acts as — the real
// schema's foreign keys require a row in users(id), which only the seeded
// user and the first migration provide without extra setup.
const sqliteActorID = ports.SeededUserID

func TestEditTransaction_WritesRecoverableRevisionRow(t *testing.T) {
	svc, clk, verify := newSQLiteTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	acc := mustAccountFixtureAs(t, svc, sqliteActorID, "HDFC Savings", "bank", "INR")
	cat := mustCategoryFixtureAs(t, svc, sqliteActorID, "Groceries", "expense")

	created, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: sqliteActorID, AccountRef: acc.Account.ID(), Amount: "800",
		CategoryRef: cat.Category.ID(), Date: "2026-08-14", Description: "Groceries",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	clk.Advance(time.Hour)
	edited, err := svc.EditTransaction(ctx, app.EditTransactionCommand{
		ActorID: sqliteActorID, TransactionRef: created.Transaction.ID(),
		AccountRef: acc.Account.ID(), Amount: "900", CategoryRef: cat.Category.ID(),
		Date: "2026-08-14", Description: "Groceries (corrected)",
	})
	if err != nil {
		t.Fatalf("EditTransaction: %v", err)
	}
	if edited.Transaction.Description() != "Groceries (corrected)" {
		t.Errorf("Description = %q, want %q", edited.Transaction.Description(), "Groceries (corrected)")
	}
	if edited.Transaction.Postings()[0].Amount().AmountMinor() != -90000 {
		t.Errorf("Amount = %d, want -90000", edited.Transaction.Postings()[0].Amount().AmountMinor())
	}

	// The revision row is a persistence-layer fact this package doesn't
	// own writing (internal/adapters/sqlite.TransactionRepository.Update
	// does, per data-model.md §7) - EditTransaction's job is just calling
	// Update with the right new state, which is what this asserts
	// actually happened, end to end.
	var revisionCount int
	var previousState string
	err = verify.QueryRowContext(ctx, `
		SELECT count(*), previous_state FROM transaction_revisions WHERE transaction_id = ?
	`, created.Transaction.ID()).Scan(&revisionCount, &previousState)
	if err != nil {
		t.Fatalf("query transaction_revisions: %v", err)
	}
	if revisionCount != 1 {
		t.Errorf("revisionCount = %d, want 1", revisionCount)
	}
	if previousState == "" {
		t.Fatal("previous_state is empty, want the pre-edit snapshot")
	}
	// "the original values are recoverable" (issue #6's "done when"): the
	// pre-edit amount and description must both be readable back out of
	// the snapshot.
	if !strings.Contains(previousState, `"description":"Groceries"`) || !strings.Contains(previousState, `"amount_minor":-80000`) {
		t.Errorf("previous_state = %s, want it to recover the pre-edit description and amount", previousState)
	}
}

// TestDeleteTransaction_GoneFromGetButPresentInDB covers the DeleteTransaction
// slice's own share of issue #6's "done when" list: "a soft-deleted
// transaction disappears from... listings but is still present in the
// database." The listings/balances half of that assertion is covered by
// TestListTransactions_ExcludesSoftDeleted and
// TestAccountBalances_ExcludesSoftDeletedTransactions once ListTransactions
// and AccountBalances exist (later slices of this same issue); this test
// covers what's available with DeleteTransaction alone: Get refuses a
// soft-deleted transaction, and the row is still physically there.
func TestDeleteTransaction_GoneFromGetButPresentInDB(t *testing.T) {
	svc, _, verify := newSQLiteTestService(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	acc := mustAccountFixtureAs(t, svc, sqliteActorID, "Cash", "cash", "USD")
	created, err := svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: sqliteActorID, AccountRef: acc.Account.ID(), Amount: "50", Description: "Coffee", Date: "2026-08-14",
	})
	if err != nil {
		t.Fatalf("RecordOutflow: %v", err)
	}

	if _, err := svc.DeleteTransaction(ctx, app.DeleteTransactionCommand{ActorID: sqliteActorID, TransactionRef: created.Transaction.ID()}); err != nil {
		t.Fatalf("DeleteTransaction: %v", err)
	}

	// Gone from Get.
	if _, _, err := svc.Transactions.Get(ctx, sqliteActorID, created.Transaction.ID()); err == nil {
		t.Error("Get after delete: want an error (soft-deleted), got nil")
	}

	// Still present in the database (data-model.md §7: rows are never
	// physically removed).
	var count int
	if err := verify.QueryRowContext(ctx, `SELECT count(*) FROM transactions WHERE id = ?`, created.Transaction.ID()).Scan(&count); err != nil {
		t.Fatalf("query transactions: %v", err)
	}
	if count != 1 {
		t.Errorf("row count for deleted transaction = %d, want 1 (still present, soft-deleted)", count)
	}
	var deletedAt *string
	if err := verify.QueryRowContext(ctx, `SELECT deleted_at FROM transactions WHERE id = ?`, created.Transaction.ID()).Scan(&deletedAt); err != nil {
		t.Fatalf("query deleted_at: %v", err)
	}
	if deletedAt == nil {
		t.Error("deleted_at is NULL, want it set")
	}
}

func mustAccountFixtureAs(t *testing.T, svc *app.Service, actorID, name, kind, currency string) app.AccountResult {
	t.Helper()
	result, err := svc.CreateAccount(context.Background(), app.CreateAccountCommand{
		ActorID: actorID, Name: name, Kind: kind, Currency: currency,
	})
	if err != nil {
		t.Fatalf("CreateAccount(%q): %v", name, err)
	}
	return result
}

func mustCategoryFixtureAs(t *testing.T, svc *app.Service, actorID, name, kind string) app.CategoryResult {
	t.Helper()
	result, err := svc.CreateCategory(context.Background(), app.CreateCategoryCommand{
		ActorID: actorID, Name: name, Kind: kind,
	})
	if err != nil {
		t.Fatalf("CreateCategory(%q): %v", name, err)
	}
	return result
}

func balanceFor(t *testing.T, result app.AccountBalancesResult, accountID string) int64 {
	t.Helper()
	for _, b := range result.Balances {
		if b.Account.ID() == accountID {
			return b.Balance.AmountMinor()
		}
	}
	t.Fatalf("no balance found for account %q", accountID)
	return 0
}
