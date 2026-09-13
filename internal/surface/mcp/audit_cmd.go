package mcp

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// defaultAuditListLimit is `bodger mcp audit`'s own default page size,
// matching internal/app.defaultMCPAuditLimit.
const defaultAuditListLimit = 50

func newMCPAuditCmd(factory ServiceFactory) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "List recent MCP tool calls bodger has recorded.",
		Long: "List the write- and destructive-tier MCP tool calls bodger has " +
			"recorded — what an agent connected over MCP has actually done to " +
			"your transactions. Read-tier calls (queries) are never recorded, so " +
			"they never appear here.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCPAudit(cmd, factory, limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", defaultAuditListLimit, "maximum number of entries to show")
	return cmd
}

func runMCPAudit(cmd *cobra.Command, factory ServiceFactory, limit int) error {
	ctx := cmd.Context()

	svc, closeDB, err := factory(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := closeDB(); cerr != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "bodger: close database:", cerr)
		}
	}()

	result, err := svc.ListMCPToolCalls(ctx, app.ListMCPToolCallsQuery{
		ActorID: ports.SeededUserID,
		Limit:   limit,
	})
	if err != nil {
		return err
	}

	renderAuditCalls(cmd.OutOrStdout(), result.Calls)
	return nil
}

// renderAuditCalls prints one line per recorded call, most recent first
// (the order ListMCPToolCalls already returns them in), plain-text —
// this command has no --json mode of its own (issue #259's scope is
// listing, not a second rendering format for a table that's a handful of
// columns wide).
func renderAuditCalls(w io.Writer, calls []ports.MCPToolCall) {
	if len(calls) == 0 {
		_, _ = fmt.Fprintln(w, "No MCP tool calls recorded yet.")
		return
	}
	for _, c := range calls {
		token := "-"
		if c.ConfirmationToken != nil {
			token = *c.ConfirmationToken
		}
		_, _ = fmt.Fprintf(w, "%s  %-10s  %-24s  %-8s  token=%s\n",
			c.CalledAt.Format("2006-01-02T15:04:05Z"), c.Tier, c.ToolName, c.Result, token)
	}
}
