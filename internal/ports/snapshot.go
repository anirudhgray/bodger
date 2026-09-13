package ports

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/budgeting"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

// SnapshotTransaction pairs one ledger.Transaction with the Tags to persist
// alongside it — the write-side mirror of internal/app's ExportedTransaction
// (ExportSnapshot's own result type). It's defined here, not reused from
// internal/app, because ports must never import the application layer
// (ADR-0005: the application layer depends on ports, never the reverse).
type SnapshotTransaction struct {
	Transaction ledger.Transaction
	Tags        []ledger.Tag
}

// Snapshot is the full actor-owned domain state SnapshotRepository.Replace
// installs, replacing whatever that actor currently has stored (issue
// #226). Every ID on every value here is expected to already be freshly
// generated and internally consistent — resolving a document's own IDs into
// fresh ones and remapping every reference between them is the application
// layer's job (ADR-0005: the application layer is the one layer that
// resolves anything ambiguous or environment-dependent, which "what should
// this restored entity's new identity be" is a case of); this port has no
// opinion on where the IDs came from, only that they're what gets written.
//
// Budgets are now included (#246), restored the same replace-wholesale way
// as everything else here. FX rates remain deliberately excluded: fx_rates
// carries no actor/user scoping at all (ADR-0004), so there is no
// per-actor ownership check that would make sense for a globally-shared
// table — a restore is defined per-actor, and fx_rates simply isn't
// per-actor data. See internal/app's ExportSnapshot (#221) for the export
// side of that same reasoning.
type Snapshot struct {
	Accounts     []ledger.Account
	Categories   []ledger.Category
	Transactions []SnapshotTransaction
	Budgets      []budgeting.Budget
}

// SnapshotRepository replaces one actor's entire ledger — every account,
// category, transaction, posting, and budget — in a single atomic database
// transaction (issue #226, ADR-0008's canonical-JSON restore). "Replace"
// means the actor's existing rows for these entities are deleted first, in
// the same transaction, then every row in snapshot is inserted; a failure
// partway through leaves the actor's prior data completely untouched —
// this is a full-state replace, never a merge, and never a partial one.
type SnapshotRepository interface {
	// Replace wipes actorID's existing accounts, categories, transactions
	// (with their postings and tags), and budgets (with their lines), then
	// writes snapshot in their place, all in one database transaction. It
	// returns a *errs.Error with code NotAllowed if any value in snapshot
	// has a UserID() other than actorID.
	Replace(ctx context.Context, actorID string, snapshot Snapshot) error
}
