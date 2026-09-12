package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// writeExportOutput writes data to outputPath when set, or to cmd's own
// stdout otherwise. Export's raw bytes are never routed through render's
// --json envelope the way every other command's result is: the canonical
// JSON export must be byte-identical for the same underlying data, and
// wrapping it in a second JSON structure this package invents would
// contradict that guarantee. This mirrors internal/surface/http's own
// unwrapped download response for the same reason (both surfaces proven
// identical by internal/surface/conformance).
func writeExportOutput(cmd *cobra.Command, outputPath string, data []byte) error {
	if outputPath == "" {
		_, err := cmd.OutOrStdout().Write(data)
		return err
	}
	if err := os.WriteFile(outputPath, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (%d bytes)\n", outputPath, len(data))
	return nil
}

// newExportJSONCmd builds "export json": issue #213's full-backup use
// case, wired onto app.ExportJSON. There is no filter here on purpose —
// the JSON export is always everything (ADR-0008's "complete... backup"),
// unlike "export csv" below.
func newExportJSONCmd(factory ServiceFactory) *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "json",
		Short: "Download a complete backup of everything you have, as JSON",
		Long: "Every account, category, and transaction you have, in one file. " +
			"This is the only format safe to restore from: re-importing it recreates exactly what was exported.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			data, err := svc.ExportJSON(ctx, app.ExportJSONQuery{ActorID: ports.SeededUserID})
			if err != nil {
				return err
			}
			return writeExportOutput(cmd, output, data)
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to this file instead of standard output")
	return cmd
}

// newExportCSVCmd builds "export csv": issue #213's CSV download,
// optionally scoped by the same filter flags `bodger report` accepts
// (bindReportFilterFlags) — omitting every filter exports every
// transaction.
func newExportCSVCmd(factory ServiceFactory) *cobra.Command {
	var output string
	var filter *reportFilterFlags
	cmd := &cobra.Command{
		Use:   "csv",
		Short: "Download your transactions as CSV",
		Long: "Flat and lossy by design: a transaction with more than one split becomes multiple rows sharing the same " +
			"date and description, and this format can't be restored from exactly — use `bodger export json` for a " +
			"complete backup instead.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			data, err := svc.ExportCSV(ctx, app.ExportCSVQuery{
				ActorID: ports.SeededUserID,
				Filter:  filter.input(),
			})
			if err != nil {
				return err
			}
			return writeExportOutput(cmd, output, data)
		},
	}
	filter = bindReportFilterFlags(cmd, true)
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to this file instead of standard output")
	return cmd
}

// newExportCmd builds "export", the parent command for issue #213's two
// download formats.
func newExportCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export your data: a complete backup, or a CSV of your transactions",
	}
	cmd.AddCommand(newExportJSONCmd(factory))
	cmd.AddCommand(newExportCSVCmd(factory))
	return cmd
}
