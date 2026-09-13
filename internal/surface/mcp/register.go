package mcp

import (
	"context"
	"fmt"
	"log/slog"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/version"
)

// ServiceFactory mirrors internal/surface/cli.ServiceFactory and
// internal/surface/http.ServiceFactory: it builds the *app.Service one
// invocation needs and a function that releases whatever it opened.
// cmd/bodger's bootstrap already has exactly this shape, so main.go
// passes it straight to Register, the same as the other two surfaces.
type ServiceFactory func(ctx context.Context) (*app.Service, func() error, error)

// Register attaches `mcp` (and its `mcp audit` subcommand) to root, the
// same way internal/surface/http.Register attaches `serve`.
func Register(root *cobra.Command, factory ServiceFactory, logger *slog.Logger) {
	root.AddCommand(newMCPCmd(factory, logger))
}

func newMCPCmd(factory ServiceFactory, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start bodger's MCP server over stdio for local tool-calling agents.",
		Long: "Start bodger's MCP server, so a local MCP client (an AI assistant or " +
			"agent) can read and act on your transactions the same way the CLI " +
			"does. Communicates over stdin/stdout — configure your MCP client to " +
			"run this command directly, not to connect over the network.\n\n" +
			"Destructive tools (deletion, bulk edits, rollback) are not registered " +
			"at all unless --allow-destructive is passed, and even then require an " +
			"explicit confirmation step before they take effect.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			allowDestructive, _ := cmd.Flags().GetBool("allow-destructive")
			return runMCP(cmd, factory, logger, allowDestructive)
		},
	}
	cmd.Flags().Bool("allow-destructive", false,
		"register destructive-tier tools (deletion, bulk edits, rollback) — disabled by default")
	cmd.AddCommand(newMCPAuditCmd(factory))
	return cmd
}

// runMCP builds the dispatcher, registers every tool Tools() returns,
// and runs the resulting server over stdio until the client disconnects
// or the context is cancelled.
func runMCP(cmd *cobra.Command, factory ServiceFactory, logger *slog.Logger, allowDestructive bool) error {
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

	dispatcher := NewDispatcher(svc, allowDestructive, logger)
	for _, def := range Tools() {
		dispatcher.Register(def)
	}

	server := dispatcher.BuildServer(&sdkmcp.Implementation{
		Name:    "bodger",
		Version: version.String(),
	})
	return server.Run(ctx, &sdkmcp.StdioTransport{})
}
