// Command bodger is the single bodger binary: serve, mcp, and every CLI
// subcommand (see "Where code goes" in the contributing guide).
//
// This file is the root command skeleton (issue #5): it loads config,
// opens the database, applies migrations, and builds the application
// layer's Service container, but registers no subcommands. Issues #7 and
// #8 add the CLI verbs and `serve` respectively, in parallel, both
// attaching to newRootCmd's tree rather than each creating their own root
// — that's why this file exists now rather than being built by either of
// them.
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
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// newRootCmd builds bodger's root cobra command. It has no subcommands
// yet: running it bootstraps the application (config, database,
// migrations, service container) and reports readiness, which is exactly
// issue #5's "done when" requirement that `go run ./cmd/bodger` starts,
// migrates a fresh database, builds the service container, and exits
// cleanly.
func newRootCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "bodger",
		Short:         "bodger is a personal finance ledger.",
		SilenceUsage:  true,
		SilenceErrors: false,
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

			// No subcommands are registered yet (issues #7 and #8 add
			// them); svc exists to prove the container wires together
			// end to end, and will be handed to those subcommands once
			// they land.
			_ = svc
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "bodger: ready — no subcommands registered yet (see issues #7 and #8)")
			return err
		},
	}
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
