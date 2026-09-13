package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// restoreSnapshotView is this package's JSON- and text-renderable shape
// for app.RestoreSnapshotResult — how many rows of each kind a restore
// installed.
type restoreSnapshotView struct {
	Accounts     int `json:"accounts"`
	Categories   int `json:"categories"`
	Transactions int `json:"transactions"`
	Budgets      int `json:"budgets"`
}

func restoreSnapshotViewFrom(r app.RestoreSnapshotResult) restoreSnapshotView {
	return restoreSnapshotView{Accounts: r.Accounts, Categories: r.Categories, Transactions: r.Transactions, Budgets: r.Budgets}
}

// readRestoreInput reads the document "restore" installs: path's
// contents when path is set, or cmd's own stdin otherwise — the mirror
// of writeExportOutput's "path, or stdout" choice (export.go).
func readRestoreInput(cmd *cobra.Command, path string) ([]byte, error) {
	if path == "" {
		return io.ReadAll(cmd.InOrStdin())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

// newRestoreCmd builds "restore": issue #227's CLI wiring for #226's
// RestoreSnapshot use case. Restoring wipes and replaces every account,
// category, and transaction you have in one shot, with no preview stage
// to lean on for safety the way the staged CSV import pipeline has — so
// this refuses outright without --yes, checked before the document is
// even read: docs/ux-principles.md §5 reserves confirmation for exactly
// this kind of genuinely destructive, hard-to-undo action, and a flag
// that must be spelled out every time is the least surprising way to ask
// for it from a non-interactive command.
func newRestoreCmd(factory ServiceFactory) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore [file]",
		Short: "Replace everything you have with a JSON backup",
		Long: "Replace every account, category, and transaction you have with the contents of a JSON backup " +
			"produced by `bodger export json`.\n\n" +
			"This cannot be undone: your current data is wiped and replaced, not merged with what the backup " +
			"contains. Reads the backup from the given file, or from standard input when no file is given. " +
			"Requires --yes; without it, nothing runs and nothing is touched.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				return errs.New(errs.InvalidInput).
					Explain("This replaces every account, category, and transaction you have with the backup's contents. Re-run with --yes to confirm.").
					Field("yes")
			}

			var path string
			if len(args) == 1 {
				path = args[0]
			}
			document, err := readRestoreInput(cmd, path)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.RestoreSnapshot(ctx, app.RestoreSnapshotQuery{ActorID: ports.SeededUserID, Document: document})
			if err != nil {
				return err
			}
			view := restoreSnapshotViewFrom(result)
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Restored %d account(s), %d category(-ies), %d transaction(s), %d budget(s).\n",
					view.Accounts, view.Categories, view.Transactions, view.Budgets)
			})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm that you want to wipe and replace all your data")
	return cmd
}
