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
)

// shutdownGrace is how long ServeCommand's graceful shutdown waits for
// in-flight requests to finish before giving up and returning anyway.
const shutdownGrace = 10 * time.Second

// Register attaches the `serve` command to root, the same way
// internal/surface/cli.Register attaches its own commands — both add to
// cmd/bodger's single root tree rather than building a second one.
// `bodger serve` starts the REST API server on
// factory(ctx)'s resulting Service.Config.HTTPBindAddr, which
// internal/platform/config has already validated (loopback, or
// authentication — M1 has no authentication, so in practice always
// loopback; see config.validateHTTPBindAddr) by the time this command
// runs.
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
			"bound anywhere else until authentication exists.",
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

	server := &http.Server{Addr: svc.Config.HTTPBindAddr, Handler: NewMux(svc, logger)}

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
