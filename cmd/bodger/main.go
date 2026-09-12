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
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/adapters/fxprovider"
	"github.com/anirudhgray/bodger/internal/adapters/sqlite"
	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	"github.com/anirudhgray/bodger/internal/platform/logging"
	"github.com/anirudhgray/bodger/internal/platform/version"
	clisurface "github.com/anirudhgray/bodger/internal/surface/cli"
	httpsurface "github.com/anirudhgray/bodger/internal/surface/http"
)

func main() {
	logger := newLogger()

	root := newRootCmd(logger)
	if err := root.Execute(); err != nil {
		jsonMode, _ := root.Flags().GetBool("json")
		os.Exit(clisurface.RenderError(os.Stderr, err, jsonMode, logger))
	}
}

// newLogger constructs the one structured logger the running binary
// uses — for a CLI invocation and for `bodger serve` alike (issue #43).
// It writes JSON to stderr: bodger is single-user and self-hosted
// (ADR-0011's Context), the operator is the person reading both a
// command's own output and its log on the same machine, and `bodger
// serve`'s existing "listening on ..." line already goes to stderr, so a
// log line landing there too costs the operator nothing new to look at.
// A rotating log file was considered and deliberately deferred — see
// docs/contributing.md's "Logging" section for why — rather than adding
// a dependency unilaterally.
//
// This is called directly from main, before newRootCmd builds anything,
// because RenderError needs a logger even when bootstrap never runs at
// all: a cobra usage error (an unknown command, a missing required flag)
// reaches Execute() without any subcommand's RunE — and therefore
// bootstrap — ever executing. Loading config here to pick the level is
// small and side-effect-free; bootstrap loads it again for the operational
// Config it actually needs, and any load failure surfaces properly there,
// as a logged *errs.Error, once a subcommand runs. A failure here just
// falls back to the default level so a logger exists regardless.
func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if cfg, err := config.Load(); err == nil {
		if lvl, perr := logging.ParseLevel(cfg.LogLevel); perr == nil {
			level = lvl
		}
	}
	return logging.New(os.Stderr, level)
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
func newRootCmd(logger *slog.Logger) *cobra.Command {
	root := &cobra.Command{
		Use:           "bodger",
		Short:         "Track where your money goes and what you have left.",
		Version:       version.String(),
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
	httpsurface.Register(root, bootstrap, logger)
	return root
}

// bootstrap performs the sequence every future subcommand needs before it
// can do anything: load config, open the database, apply pending
// migrations, and construct the application-layer Service container. It
// returns a close function the caller must invoke once the database is no
// longer needed.
//
// Every failure here becomes a *errs.Error before it's returned, the same
// way internal/app's own construction failures do (missingDependency in
// internal/app/service.go) — this package sits outside internal/app, so
// internal/lint's bare-error check (issue #12) doesn't reach it, but a
// config path, a database file path, or a driver's migration error is
// exactly the kind of internal detail that must not print to the
// terminal unfiltered. RenderError's fallback branch (internal/surface/cli/cli.go)
// still prints a non-*errs.Error as-is, but only cobra's own usage errors
// (an unknown command, a missing required flag) reach it now — those are
// already user-safe by construction, which bootstrapError isn't.
func bootstrap(ctx context.Context) (*app.Service, func() error, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, bootstrapError("bodger couldn't load its configuration.", err)
	}

	clk := clock.New()

	db, err := sqlite.Open(clk, cfg.DBPath)
	if err != nil {
		return nil, nil, bootstrapError("bodger couldn't open its database.", err)
	}

	if err := db.MigrateUp(ctx); err != nil {
		_ = db.Close()
		return nil, nil, bootstrapError("bodger couldn't prepare its database.", err)
	}

	svc, err := app.NewService(
		clk,
		cfg,
		idgen.New(),
		sqlite.NewAccountRepository(db),
		sqlite.NewCategoryRepository(db),
		sqlite.NewTransactionRepository(db),
		sqlite.NewTagRepository(db),
		sqlite.NewUserRepository(db),
		sqlite.NewSessionRepository(db),
		sqlite.NewAPITokenRepository(db),
		sqlite.NewFxRateRepository(db),
		fxprovider.New(cfg.FxProviderBaseURL, nil),
		sqlite.NewImportBatchRepository(db),
		sqlite.NewImportRecordRepository(db),
	)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	return svc, db.Close, nil
}

// bootstrapError renders a bootstrap failure the way every other internal
// failure in this codebase is rendered (ADR-0011): a safe, generic
// message a surface can show, with cause wrapped for the log rather than
// printed. Internal, not a more specific code, because nothing about a
// bootstrap failure is something the user did wrong.
func bootstrapError(message string, cause error) error {
	return errs.New(errs.Internal).Explain("%s Check the log for what went wrong.", message).Wrap(cause)
}
