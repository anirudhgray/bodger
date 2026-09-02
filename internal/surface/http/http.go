// Package http is bodger's REST API surface (issue #8): every handler
// follows the same adapter contract docs/decisions/0005-shared-application-layer.md
// describes and internal/surface/cli already follows — decode the
// request into an internal/app command or query struct, call exactly one
// application-layer method, and encode the result. Nothing in this
// package parses a date or an amount, and nothing asks the wall clock
// what time it is; a missing "date" field on a request is passed through
// to the application layer as an empty string, and the application layer
// (internal/app/normalize.DateOf) is what resolves it to "today" in the
// actor's timezone. CI is meant to fail if this package ever reads the
// wall clock directly itself — see internal/lint's banned-symbol check
// (issue #9), which now covers every surface, not just this one.
//
// This package never imports internal/domain or its subpackages
// (internal/domain/ledger, internal/domain/money) — docs/architecture.md
// §3 reserves that entirely to internal/app. Every rendered value below is
// still built from real domain values (an Account's name, a Money's
// formatted amount, and so on): commands and results returned by
// internal/app already carry those as fields, and Go lets this package
// call their accessor methods (Name(), AmountString(), ...) through
// ordinary type inference without ever spelling out the defining
// package's type name — the same trick internal/surface/cli uses.
//
// Routes (docs/architecture.md §5, issue #8):
//
//	GET/POST    /api/v1/accounts        GET/PATCH/DELETE  /api/v1/accounts/{id}
//	GET/POST    /api/v1/categories      GET/PATCH/DELETE  /api/v1/categories/{id}
//	GET/POST    /api/v1/transactions    GET/PATCH/DELETE  /api/v1/transactions/{id}
//	POST        /api/v1/transfers
//	GET         /api/v1/balances
//	GET         /healthz
//
// There is no authentication yet (M1, ADR-0006): every request acts as
// the single seeded user, ports.SeededUserID. This package binds
// loopback only by default, and internal/platform/config refuses to load
// a configuration that binds anywhere else — see ServeCommand and
// internal/platform/config's validateHTTPBindAddr.
//
// One route bends the "decode, call one app method, encode" rule in a
// narrow, documented way: PATCH on a single account or category accepts a
// partial body expressing exactly one of several sibling update
// intents (rename an account vs. re-declare its opening balance; rename a
// category vs. reparent it) — the application layer exposes these as
// separate use-case methods, so the handler picks which one to call based
// on which JSON keys are present. That selection is presentation-layer
// routing, the same way choosing which cobra subcommand to run is: it
// decides *which* application method answers the request, never whether
// the request is valid or what a value should become. See accounts.go's
// and categories.go's patch handlers.
package http

import (
	"context"
	"log/slog"

	"github.com/anirudhgray/bodger/internal/app"
)

// ServiceFactory builds the application-layer Service a single request
// (or, in practice, the server's whole lifetime) needs, and returns a
// function that releases whatever it opened once the server shuts down.
// cmd/bodger's bootstrap function already has exactly this shape — the
// same one internal/surface/cli.ServiceFactory declares — so main.go
// passes it straight to Register with no adapting.
type ServiceFactory func(ctx context.Context) (*app.Service, func() error, error)

// handlers holds the single *app.Service every handler method in this
// package calls into, and the logger respondError (respond.go) logs an
// *errs.Error's cause chain through before rendering its safe message
// (ADR-0011; issue #43). There is no per-request state beyond the
// request itself: both, once built, are shared by every request the
// server handles for its whole lifetime.
type handlers struct {
	svc    *app.Service
	logger *slog.Logger
}
