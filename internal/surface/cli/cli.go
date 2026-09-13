// Package cli is bodger's command-line surface (issue #7). Every command
// here follows the adapter contract docs/decisions/0005-shared-application-layer.md
// describes: decode cobra flags into an internal/app command struct, call
// exactly one application-layer method, and render the result. Nothing in
// this package parses a date or an amount, and nothing asks the wall
// clock what time it is — those are the application layer's job, and CI
// is meant to fail if either happens here (see the package's grep-checked
// "done when" item in issue #7).
//
// This package never imports internal/domain or its subpackages
// (internal/domain/ledger, internal/domain/money) — docs/architecture.md
// §3 reserves that entirely to internal/app. Every rendered value below is
// still built from real domain values (an Account's name, a Money's
// formatted amount, and so on): commands and results returned by
// internal/app already carry those as fields, and Go lets this package
// call their accessor methods (Name(), String(), AmountString(), ...)
// through ordinary type inference without ever spelling out the
// defining package's type name. That's what keeps every view type in this
// package built from plain strings/ints/bools while still describing a
// real domain value's shape.
//
// One command bends the "call exactly one application method" rule on
// purpose: resolveDefaultAccount, used by spend and receive when --account
// is omitted (see entries.go). It calls ListAccounts first to see whether
// the actor has exactly one account, and if so uses it — this is the
// account-defaulting behaviour docs/ux-principles.md §3 and §7 call for
// ("account defaults... to the only one if there's one"), and issue #7
// calls it out explicitly as in scope despite the general rule. Every
// other command in this package touches the application layer exactly
// once.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// ServiceFactory builds the application-layer Service a single CLI
// invocation needs, and returns a function that releases whatever it
// opened (a database connection, in production) once the command is
// done. cmd/bodger's bootstrap function already has exactly this shape,
// so main.go passes it straight to Register with no adapting.
type ServiceFactory func(ctx context.Context) (*app.Service, func() error, error)

// Register attaches every command this package owns to root, and defines
// the --json flag every data-returning command reads (issue #7's
// "progressive disclosure" escape hatch — docs/ux-principles.md §4).
// --json is a persistent flag on root rather than repeated on every leaf
// command, so it works the same way no matter where in the command tree
// it's passed.
func Register(root *cobra.Command, factory ServiceFactory) {
	root.PersistentFlags().Bool("json", false, "print machine-readable JSON instead of plain text")

	root.AddCommand(newSpendCmd(factory))
	root.AddCommand(newReceiveCmd(factory))
	root.AddCommand(newMoveCmd(factory))
	root.AddCommand(newBalanceCmd(factory))
	root.AddCommand(newTransactionsCmd(factory))
	root.AddCommand(newAccountsCmd(factory))
	root.AddCommand(newCategoriesCmd(factory))
	root.AddCommand(newAuthCmd(factory))
	root.AddCommand(newFxCmd(factory))
	root.AddCommand(newConfigCmd(factory))
	root.AddCommand(newReportCmd(factory))
	root.AddCommand(newExportCmd(factory))
	root.AddCommand(newImportCmd(factory))
	root.AddCommand(newRestoreCmd(factory))
	root.AddCommand(newBudgetsCmd(factory))
}

// jsonRequested reports whether --json was passed anywhere in the command
// chain that led to cmd. Cobra merges a persistent flag defined on an
// ancestor into every descendant's own flag set once that descendant
// actually runs, so this is safe to call from any leaf command's RunE.
func jsonRequested(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("json")
	return v
}

// jsonEnvelope is the one stable shape every successful --json response
// uses (issue #7: "pick a consistent envelope and use it everywhere").
type jsonEnvelope struct {
	Data any `json:"data"`
}

// jsonErrorEnvelope is --json's error counterpart, used by RenderError.
type jsonErrorEnvelope struct {
	Error *errs.Error `json:"error"`
}

// writeJSON encodes v inside jsonEnvelope and writes it to w, terminated
// with a newline (encoding/json.Encoder always appends one).
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonEnvelope{Data: v})
}

// render writes data to cmd's stdout: as JSON (via writeJSON) when --json
// was passed, or via human, the caller-supplied plain-text renderer,
// otherwise. Every leaf command in this package funnels its successful
// result through this one function, which is what keeps the --json
// envelope identical across all of them.
func render(cmd *cobra.Command, data any, human func(io.Writer)) error {
	if jsonRequested(cmd) {
		return writeJSON(cmd.OutOrStdout(), data)
	}
	human(cmd.OutOrStdout())
	return nil
}

// closeQuietly releases what factory opened, logging (rather than
// failing the command over) a close error — the command's own result has
// already been decided by the time defer runs this, so a close failure
// shouldn't overwrite it. This mirrors cmd/bodger's existing bootstrap
// error-handling pattern (main.go), extended to every subcommand rather
// than only the bare root command.
func closeQuietly(cmd *cobra.Command, closeDB func() error) {
	if err := closeDB(); err != nil {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "bodger: close database:", err)
	}
}

// RenderError renders err the way a CLI invocation should report it and
// returns the process exit code main.go should use. When err is an
// *errs.Error (the shared validation/domain error type issue #4 built,
// see internal/platform/errs), this logs its full cause chain through
// logger before printing anything — logger.Error dispatches to
// (*errs.Error).LogValue on its own (internal/platform/logging's doc
// comment), so the SQL fragment, driver text, or wrapped fmt.Errorf that
// caused an Internal error is captured somewhere a self-hoster can find
// it (ADR-0011; issue #43) — then prints its safe, user-facing message —
// as JSON when jsonMode is set — and returns its registered CLI exit
// code.
//
// Anything else is printed as-is with exit code 1, and not logged: in
// practice that's only ever a cobra usage error (an unknown command, a
// missing required flag) — every other error reaching this far is
// already a *errs.Error: internal/app returns nothing else
// (internal/lint's check, issue #12), adapters return nothing else (the
// ports contract), and main.go's own bootstrap wraps its failures the
// same way (issue #39). Cobra's usage errors are safe to print verbatim
// by construction, and carry no cause chain worth logging, which is what
// this fallback exists for — it is not a place a raw internal error is
// expected to end up.
func RenderError(w io.Writer, err error, jsonMode bool, logger *slog.Logger) int {
	var e *errs.Error
	if errors.As(err, &e) {
		if logger != nil {
			logger.Error("command failed", "error", e)
		}
		if jsonMode {
			_ = writeJSONError(w, e)
		} else {
			_, _ = fmt.Fprintln(w, e.CLIMessage())
		}
		return e.CLIExitCode()
	}
	_, _ = fmt.Fprintln(w, "bodger:", err)
	return 1
}

func writeJSONError(w io.Writer, e *errs.Error) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonErrorEnvelope{Error: e})
}
