// Package cli is bodger's command-line surface (issue #7). Every command
// here follows the adapter contract docs/decisions/0005-shared-application-layer.md
// describes: decode cobra flags into an internal/app command struct, call
// exactly one application-layer method, and render the result. Nothing in
// this package parses a date, an amount, or a currency, and nothing calls
// time.Now() — those are the application layer's job, and CI is meant to
// fail if either happens here (see the package's grep-checked "done when"
// item in issue #7).
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
	root.AddCommand(newAccountsCmd(factory))
	root.AddCommand(newCategoriesCmd(factory))
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
		fmt.Fprintln(cmd.ErrOrStderr(), "bodger: close database:", err)
	}
}

// RenderError renders err the way a CLI invocation should report it and
// returns the process exit code main.go should use. When err is an
// *errs.Error (the shared validation/domain error type issue #4 built,
// see internal/platform/errs), this prints its safe, user-facing message
// — as JSON when jsonMode is set — and returns its registered CLI exit
// code. Anything else (a cobra usage error: an unknown command, a missing
// required flag) is printed as-is with exit code 1, since those never
// carry a registered code to report instead.
func RenderError(w io.Writer, err error, jsonMode bool) int {
	var e *errs.Error
	if errors.As(err, &e) {
		if jsonMode {
			_ = writeJSONError(w, e)
		} else {
			fmt.Fprintln(w, e.CLIMessage())
		}
		return e.CLIExitCode()
	}
	fmt.Fprintln(w, "bodger:", err)
	return 1
}

func writeJSONError(w io.Writer, e *errs.Error) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonErrorEnvelope{Error: e})
}
