// Command bodger is the single bodger binary: serve, mcp, and every CLI
// subcommand (see "Where code goes" in the contributing guide).
//
// This file is the root command skeleton (issue #5): it loads config,
// opens the database, applies migrations, and builds the application
// layer's Service container. internal/surface/cli (issue #7) attaches the
// account, category, transaction, and balance commands to newRootCmd's
// tree via Register, and internal/surface/http (issue #8) attaches
// `serve` the same way — both add to this tree rather than creating
// their own root.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/adapters/sqlite"
	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	clisurface "github.com/anirudhgray/bodger/internal/surface/cli"
	httpsurface "github.com/anirudhgray/bodger/internal/surface/http"
)

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		jsonMode, _ := root.Flags().GetBool("json")
		os.Exit(clisurface.RenderError(os.Stderr, err, jsonMode))
	}
}

// newRootCmd builds bodger's root cobra command and attaches every CLI
// subcommand issue #7 built (internal/surface/cli.Register). Running the
// binary with no subcommand still bootstraps the application (config,
// database, migrations, service container) and reports readiness, which
// is issue #5's original "done when" requirement — kept as-is so it
// continues to prove the container wires together end to end even now
// that real subcommands exist.
//
// SilenceErrors is true so this package controls error rendering itself
// (main, above): an error returned by Execute is rendered through
// internal/surface/cli.RenderError, which knows how to print an
// *errs.Error's safe message and exit code rather than cobra's own
// generic "Error: ..." formatting.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "bodger",
		Short:         "bodger is a personal finance ledger.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, closeDB, err := bootstrap(cmd.Context())
			if err != nil {
				return err
			}
			defer func() {
				if cerr := closeDB(); cerr != nil {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "bodger: close database:", cerr)
				}
			}()

			_ = svc
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "bodger: ready — run `bodger --help` to see what you can do")
			return err
		},
	}

	clisurface.Register(root, bootstrap)
	httpsurface.Register(root, bootstrap)
	return root
}

// bootstrap performs the sequence every future subcommand needs before it
// can do anything: load config, open the database, apply pending
// migrations, and construct the application-layer Service container. It
// returns a close function the caller must invoke once the database is no
// longer needed.
func bootstrap(ctx context.Context) (*app.Service, func() error, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("bodger: load config: %w", err)
	}

	clk := clock.New()

	db, err := sqlite.Open(clk, cfg.DBPath)
	if err != nil {
		return nil, nil, fmt.Errorf("bodger: open database %q: %w", cfg.DBPath, err)
	}

	if err := db.MigrateUp(ctx); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("bodger: apply migrations: %w", err)
	}

	svc, err := app.NewService(
		clk,
		cfg,
		idgen.New(),
		sqlite.NewAccountRepository(db),
		sqlite.NewCategoryRepository(db),
		sqlite.NewTransactionRepository(db),
		sqlite.NewTagRepository(db),
	)
	if err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("bodger: build service container: %w", err)
	}

	return svc, db.Close, nil
}
