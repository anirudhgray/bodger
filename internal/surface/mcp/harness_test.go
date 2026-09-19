package mcp

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/adapters/fxprovider"
	"github.com/anirudhgray/bodger/internal/adapters/sqlite"
	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	"github.com/anirudhgray/bodger/internal/ports"
)

// newTestService wires an *app.Service to the real SQLite adapter over a
// fresh, fully-migrated temp-file database, frozen at frozenAt — the same
// wiring internal/surface/http/http_test.go's and
// internal/surface/cli/cli_test.go's own newTestService/newTestFactory
// helpers use, reproduced here since cmd/bodger's real bootstrap lives in
// package main and can't be imported from a test in this package. It
// wires a real (network-touching) Frankfurter provider — fine for every
// test that never calls fetch_fx_rates; a test that does calls
// newTestServiceWithFxProvider instead so it never depends on the network
// (see fx_write_test.go), mirroring internal/surface/http/http_test.go's
// own newTestService/newTestServiceWithFxProvider split.
func newTestService(t *testing.T, frozenAt time.Time) (*app.Service, *clock.Frozen) {
	t.Helper()
	return newTestServiceWithFxProvider(t, frozenAt, fxprovider.New("", nil))
}

// newTestServiceWithFxProvider is newTestService with the FX rate
// provider swapped out.
func newTestServiceWithFxProvider(t *testing.T, frozenAt time.Time, provider ports.FxRateProvider) (*app.Service, *clock.Frozen) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "bodger.db")
	clk := clock.NewFrozen(frozenAt)

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

	svc, err := app.NewService(
		clk, config.Defaults, idgen.New(),
		sqlite.NewAccountRepository(db), sqlite.NewCategoryRepository(db),
		sqlite.NewTransactionRepository(db), sqlite.NewTagRepository(db),
		sqlite.NewUserRepository(db), sqlite.NewSessionRepository(db), sqlite.NewAPITokenRepository(db),
		sqlite.NewFxRateRepository(db), provider,
		sqlite.NewImportBatchRepository(db), sqlite.NewImportRecordRepository(db), sqlite.NewImportCommitRepository(db),
		sqlite.NewSnapshotRepository(db),
		sqlite.NewBudgetRepository(db),
		sqlite.NewRecurringRuleRepository(db),
		sqlite.NewScheduledOccurrenceRepository(db),
		sqlite.NewRecurringMaterializationRepository(db),
		sqlite.NewMCPToolCallRepository(db),
	)
	if err != nil {
		t.Fatalf("app.NewService: %v", err)
	}
	return svc, clk
}
