package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/config"
)

// shutdownGrace is how long ServeCommand's graceful shutdown waits for
// in-flight requests to finish before giving up and returning anyway.
const shutdownGrace = 10 * time.Second

// Register attaches the `serve` command to root, the same way
// internal/surface/cli.Register attaches its own commands — both add to
// cmd/bodger's single root tree rather than building a second one.
// `bodger serve` starts the REST API server on factory(ctx)'s resulting
// Service.Config.HTTPBindAddr, which internal/platform/config has already
// validated as structurally well-formed; runServe's own checkBindAddr is
// what additionally refuses a non-loopback bind until authentication is
// configured (ADR-0006, issue #56).
//
// logger is threaded through to NewMux so every handler's respondError
// (respond.go) logs an *errs.Error's cause chain before rendering its
// safe response (ADR-0011; issue #43) — cmd/bodger constructs it once
// and passes it here the same way it passes bootstrap.
func Register(root *cobra.Command, factory ServiceFactory, logger *slog.Logger) {
	root.AddCommand(newServeCmd(factory, logger))
}

func newServeCmd(factory ServiceFactory, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the REST API server.",
		Long: "Start bodger's REST API server, the interface the web UI and external " +
			"clients use. Binds to a loopback address by default, and refuses to start " +
			"bound anywhere else until a password has been set (run `bodger auth set-password`).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, factory, logger)
		},
	}
}

func runServe(cmd *cobra.Command, factory ServiceFactory, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc, closeDB, err := factory(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := closeDB(); cerr != nil {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "bodger: close database:", cerr)
		}
	}()

	if err := checkBindAddr(ctx, svc); err != nil {
		return err
	}

	server := &http.Server{Addr: svc.Config.HTTPBindAddr, Handler: NewServerHandler(svc, logger)}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.ListenAndServe()
	}()

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "bodger: listening on %s\n", svc.Config.HTTPBindAddr)

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server: %w", err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down server: %w", err)
		}
		return nil
	}
}

// checkBindAddr enforces the non-loopback half of ADR-0006's refuse-to-start
// rule that internal/platform/config's validateHTTPBindAddr used to enforce
// entirely on its own: a loopback bind is always fine; a non-loopback one
// now requires a password to already be set on the seeded user (issue #56 —
// "now that auth exists, allow a non-loopback bind once it's configured").
// This lives here, not in internal/platform/config, because "is
// authentication configured" is database state that package has no access
// to (ADR-0005: it only reads the environment) — this function has the
// *app.Service that can actually answer the question.
func checkBindAddr(ctx context.Context, svc *app.Service) error {
	loopback, err := config.IsLoopback(svc.Config.HTTPBindAddr)
	if err != nil {
		return fmt.Errorf("bodger: %w", err)
	}
	if loopback {
		return nil
	}

	configured, err := svc.IsAuthConfigured(ctx)
	if err != nil {
		return fmt.Errorf("bodger: check whether authentication is configured: %w", err)
	}
	if !configured {
		return fmt.Errorf(
			"bodger: %s binds a non-loopback address, and no password has been set yet — "+
				"run `bodger auth set-password` first, or bind to a loopback address instead "+
				"(127.0.0.1, ::1, or localhost)",
			svc.Config.HTTPBindAddr,
		)
	}
	return nil
}
