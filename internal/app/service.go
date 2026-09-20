// Package app is bodger's application layer (ADR-0005): the only layer
// that knows what "now" is, the only layer that resolves currency
// precedence, and the layer every surface — CLI, REST API, MCP, and
// (through the REST API) the web UI — calls into, in-process, for every
// user intent.
//
// This file builds Service, the container every use-case method (issue
// #6) will hang off of: the clock, resolved config, an ID generator, and
// every repository the app layer needs. It is constructed exactly once
// per process, in cmd/bodger, and shared by every surface that process
// registers.
package app

import (
	"fmt"

	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	"github.com/anirudhgray/bodger/internal/ports"
)

// Service is bodger's application-layer container. A surface holds a
// *Service and calls its (future, issue #6) use-case methods; nothing
// outside this package constructs a Service's fields directly, and
// nothing outside internal/app reaches into ports or platform to build
// its own copy of "the current time" or "the account repository".
//
// This struct is a known parallel-work collision point (issue #5's
// tracking note): it gains a field, and NewService gains a parameter,
// with most features landing after M1. Keep additions here small,
// additive, and reviewed in isolation from whatever feature motivated
// them, so two features growing this struct in parallel branches produce
// a mechanical merge conflict rather than a silent behavioural one.
type Service struct {
	// Clock is the injected time source every use-case method resolves
	// "now" through (ADR-0005). Production wires clock.New(); tests wire
	// clock.NewFrozen(...).
	Clock clock.Clock
	// Config is bodger's resolved instance configuration — the bottom
	// rung of the currency precedence ladder (normalize.Currency) and the
	// user's timezone (normalize.DateOf), among whatever else lands here
	// as M1 continues.
	Config config.Config
	// IDs generates identifiers for newly created domain entities.
	// Production wires idgen.New(); tests wire idgen.NewSequence(...) for
	// deterministic assertions.
	IDs idgen.Generator

	// Accounts, Categories, Transactions, and Tags are the repository
	// ports issue #6's use-case methods read and write through. The
	// application layer depends only on these interfaces
	// (internal/ports) — never on internal/adapters/sqlite directly —
	// which is what makes the persistence layer swappable without this
	// package noticing (ADR-0007).
	Accounts     ports.AccountRepository
	Categories   ports.CategoryRepository
	Transactions ports.TransactionRepository
	Tags         ports.TagRepository

	// Users, Sessions, and APITokens are issue #55's auth use cases'
	// repository ports (ADR-0006): reading and writing a user's password
	// hash, and issuing/authenticating/revoking session cookies and API
	// tokens.
	Users     ports.UserRepository
	Sessions  ports.SessionRepository
	APITokens ports.APITokenRepository

	// FxRates is issue #130's repository port over fx_rates: storing
	// fetched exchange rates and looking one up via #129's
	// nearest-earlier-within-staleness-window rule. Unlike every other
	// field above, it carries no actor scoping — fx_rates is a global
	// table (ADR-0004).
	FxRates ports.FxRateRepository

	// FxProvider is issue #131's external rate source (ADR-0012:
	// Frankfurter). FetchFxRates (issue #135) is the only use-case method
	// that ever calls it — every other use case reads exclusively through
	// FxRates, never this, so the application stays fully usable with no
	// network per ADR-0004.
	FxProvider ports.FxRateProvider

	// ImportBatches and ImportRecords are issue #208's repository ports
	// over import_batch/import_record (ADR-0008): the staged import
	// pipeline's state. StageImport (#210) reads and writes both;
	// CommitImportBatch and RollbackImportBatch (#211) additionally read
	// them to decide what a commit or rollback affects.
	ImportBatches ports.ImportBatchRepository
	ImportRecords ports.ImportRecordRepository

	// ImportCommits is issue #211's repository port over the one write
	// ADR-0008 requires to be atomic: writing a batch's resolved records'
	// transactions in a single database transaction, and its mirror,
	// rolling a committed batch back.
	ImportCommits ports.ImportCommitRepository

	// Snapshots is issue #226's repository port: replacing an actor's
	// entire ledger in one atomic transaction from a restored
	// bodger.export/v1 document (RestoreSnapshot, restore.go). Unlike
	// ImportBatches/ImportRecords above, this one is wired in with its
	// first use-case method already using it, not ahead of one.
	Snapshots ports.SnapshotRepository

	// Budgets is issue #241's repository port over budgets/budget_lines
	// (data-model.md §10): a Budget as a plan, kept strictly separate
	// from what actually happened. Following the ImportBatches/
	// ImportRecords precedent above, this is wired in ahead of its first
	// use-case method — CRUD (#242) and actuals/reporting (#243) are
	// separate, later issues.
	Budgets ports.BudgetRepository

	// RecurringRules and ScheduledOccurrences are issue #276's repository
	// ports over recurring_rules/scheduled_occurrences (data-model.md §11,
	// ADR-0014): a rule as a template and an occurrence as a projection,
	// neither of which is money that moved. Following the
	// ImportBatches/ImportRecords precedent above, both are wired in ahead
	// of their first use-case method — rule CRUD (#277), occurrence
	// generation (#278), and materialisation (#279) are separate, later
	// issues.
	//
	// Nothing on either port returns a posting, a transaction, or a
	// monetary value, and no balance or analytics method reads through
	// them: that is the structural half of "an occurrence never
	// contributes to a balance" (ADR-0014).
	RecurringRules       ports.RecurringRuleRepository
	ScheduledOccurrences ports.ScheduledOccurrenceRepository

	// RecurringMaterializations is issue #279's repository port over the
	// one write that must be atomic: turning a pending ScheduledOccurrence
	// into a real Transaction. Separate from RecurringRules/
	// ScheduledOccurrences above for the same reason ImportCommits is
	// separate from ImportBatches/ImportRecords — it is the one operation
	// that writes to transactions/postings and scheduled_occurrences
	// together, in a single database transaction.
	RecurringMaterializations ports.RecurringMaterializationRepository

	// MCPToolCalls is issue #259's repository port over mcp_tool_call
	// (ADR-0013): the audit trail of every write- and destructive-tier
	// MCP tool invocation. Following the ImportBatches/ImportRecords
	// precedent above, this is wired in ahead of any real tool that
	// writes through it — issue #259 itself only ships a read-tier
	// "whoami" tool to prove the transport; the financial tools that
	// actually populate this table are #260/#261/#262.
	MCPToolCalls ports.MCPToolCallRepository

	// Suggestions is M10 · Valinor's external AI-suggestion source
	// (ADR-0015: typesafe.ai). Following the ImportBatches/Budgets/
	// RecurringRules precedent above, it is wired in ahead of its first
	// use-case method — SuggestForImportBatch is a later, separate issue.
	//
	// Unlike every repository field on this struct, nil is a legitimate
	// production value here, not a construction error: an unconfigured
	// instance (no BODGER_TYPESAFE_API_KEY, config.go) runs with
	// Suggestions unset, and the feature is simply off. NewService
	// deliberately does not require or nil-check this field, the way it
	// does every repository above — a repository's absence means bodger
	// can't start; this field's absence means one optional feature can't
	// run, which must never be a startup failure (ADR-0015 "no key
	// anywhere... must never prevent the binary from starting").
	// SuggestForImportBatch's own issue is what defines the no-op/absent
	// behaviour a caller sees when this is nil.
	Suggestions ports.SuggestionProvider
}

// NewService constructs a Service from its dependencies, rejecting a nil
// argument rather than deferring the failure to whichever use-case method
// happens to dereference it first. Every argument is required: there is
// no partially-constructed Service.
func NewService(
	clk clock.Clock,
	cfg config.Config,
	ids idgen.Generator,
	accounts ports.AccountRepository,
	categories ports.CategoryRepository,
	transactions ports.TransactionRepository,
	tags ports.TagRepository,
	users ports.UserRepository,
	sessions ports.SessionRepository,
	apiTokens ports.APITokenRepository,
	fxRates ports.FxRateRepository,
	fxProvider ports.FxRateProvider,
	importBatches ports.ImportBatchRepository,
	importRecords ports.ImportRecordRepository,
	importCommits ports.ImportCommitRepository,
	snapshots ports.SnapshotRepository,
	budgets ports.BudgetRepository,
	recurringRules ports.RecurringRuleRepository,
	scheduledOccurrences ports.ScheduledOccurrenceRepository,
	recurringMaterializations ports.RecurringMaterializationRepository,
	mcpToolCalls ports.MCPToolCallRepository,
) (*Service, error) {
	switch {
	case clk == nil:
		return nil, missingDependency("clock")
	case ids == nil:
		return nil, missingDependency("id generator")
	case accounts == nil:
		return nil, missingDependency("account repository")
	case categories == nil:
		return nil, missingDependency("category repository")
	case transactions == nil:
		return nil, missingDependency("transaction repository")
	case tags == nil:
		return nil, missingDependency("tag repository")
	case users == nil:
		return nil, missingDependency("user repository")
	case sessions == nil:
		return nil, missingDependency("session repository")
	case apiTokens == nil:
		return nil, missingDependency("API token repository")
	case fxRates == nil:
		return nil, missingDependency("FX rate repository")
	case fxProvider == nil:
		return nil, missingDependency("FX rate provider")
	case importBatches == nil:
		return nil, missingDependency("import batch repository")
	case importRecords == nil:
		return nil, missingDependency("import record repository")
	case importCommits == nil:
		return nil, missingDependency("import commit repository")
	case snapshots == nil:
		return nil, missingDependency("snapshot repository")
	case budgets == nil:
		return nil, missingDependency("budget repository")
	case recurringRules == nil:
		return nil, missingDependency("recurring rule repository")
	case scheduledOccurrences == nil:
		return nil, missingDependency("scheduled occurrence repository")
	case recurringMaterializations == nil:
		return nil, missingDependency("recurring materialization repository")
	case mcpToolCalls == nil:
		return nil, missingDependency("MCP tool call repository")
	}

	return &Service{
		Clock:         clk,
		Config:        cfg,
		IDs:           ids,
		Accounts:      accounts,
		Categories:    categories,
		Transactions:  transactions,
		Tags:          tags,
		Users:         users,
		Sessions:      sessions,
		APITokens:     apiTokens,
		FxRates:       fxRates,
		FxProvider:    fxProvider,
		ImportBatches: importBatches,
		ImportRecords: importRecords,
		ImportCommits: importCommits,
		Snapshots:     snapshots,
		Budgets:       budgets,

		RecurringRules:       recurringRules,
		ScheduledOccurrences: scheduledOccurrences,

		RecurringMaterializations: recurringMaterializations,

		MCPToolCalls: mcpToolCalls,
	}, nil
}

// missingDependency reports a nil constructor argument as a registry error
// rather than a bare fmt.Errorf, so that a mis-wired binary fails the same
// way every other app-layer failure does — with a code a surface can turn
// into an exit code or a status (ADR-0011).
//
// The code is Internal because a nil dependency can only come from
// cmd/bodger wiring the container wrongly; nothing a user typed can cause
// it, and telling them which of bodger's internal parts is missing would
// be an internal detail they can't act on. So the dependency's name goes
// into the wrapped cause — logged in full, never serialised — and the user
// gets a message that says what happened and where to look. This is also
// the shape internal/lint's bare-error check exists to require: a
// fmt.Errorf inside Wrap is exactly right, the same call returned directly
// is not.
func missingDependency(name string) error {
	return errs.New(errs.Internal).
		Explain("bodger couldn't start because it isn't set up correctly. Check the log for what went wrong.").
		Wrap(fmt.Errorf("app: %s must not be nil", name))
}
