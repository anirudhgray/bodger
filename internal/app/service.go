// Package app is bodger's application layer (ADR-0005): the only layer
// that knows what "now" is, the only layer that resolves currency
// precedence, and the layer every surface — CLI, REST API, MCP, and
// (through the REST API) the web UI — calls into, in-process, for every
// user intent.
//
// This file builds Service, the container every use-case method (issue
// #6) will hang off of: the clock, resolved config, an ID generator, and
// every repository the app layer needs. It is constructed exactly once
// per process, in cmd/bodger, and shared by every surface that process
// registers.
package app

import (
	"fmt"

	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	"github.com/anirudhgray/bodger/internal/ports"
)

// Service is bodger's application-layer container. A surface holds a
// *Service and calls its (future, issue #6) use-case methods; nothing
// outside this package constructs a Service's fields directly, and
// nothing outside internal/app reaches into ports or platform to build
// its own copy of "the current time" or "the account repository".
//
// This struct is a known parallel-work collision point (issue #5's
// tracking note): it gains a field, and NewService gains a parameter,
// with most features landing after M1. Keep additions here small,
// additive, and reviewed in isolation from whatever feature motivated
// them, so two features growing this struct in parallel branches produce
// a mechanical merge conflict rather than a silent behavioural one.
type Service struct {
	// Clock is the injected time source every use-case method resolves
	// "now" through (ADR-0005). Production wires clock.New(); tests wire
	// clock.NewFrozen(...).
	Clock clock.Clock
	// Config is bodger's resolved instance configuration — the bottom
	// rung of the currency precedence ladder (normalize.Currency) and the
	// user's timezone (normalize.DateOf), among whatever else lands here
	// as M1 continues.
	Config config.Config
	// IDs generates identifiers for newly created domain entities.
	// Production wires idgen.New(); tests wire idgen.NewSequence(...) for
	// deterministic assertions.
	IDs idgen.Generator

	// Accounts, Categories, Transactions, and Tags are the repository
	// ports issue #6's use-case methods read and write through. The
	// application layer depends only on these interfaces
	// (internal/ports) — never on internal/adapters/sqlite directly —
	// which is what makes the persistence layer swappable without this
	// package noticing (ADR-0007).
	Accounts     ports.AccountRepository
	Categories   ports.CategoryRepository
	Transactions ports.TransactionRepository
	Tags         ports.TagRepository
}

// NewService constructs a Service from its dependencies, rejecting a nil
// argument rather than deferring the failure to whichever use-case method
// happens to dereference it first. Every argument is required: there is
// no partially-constructed Service.
func NewService(
	clk clock.Clock,
	cfg config.Config,
	ids idgen.Generator,
	accounts ports.AccountRepository,
	categories ports.CategoryRepository,
	transactions ports.TransactionRepository,
	tags ports.TagRepository,
) (*Service, error) {
	switch {
	case clk == nil:
		return nil, missingDependency("clock")
	case ids == nil:
		return nil, missingDependency("id generator")
	case accounts == nil:
		return nil, missingDependency("account repository")
	case categories == nil:
		return nil, missingDependency("category repository")
	case transactions == nil:
		return nil, missingDependency("transaction repository")
	case tags == nil:
		return nil, missingDependency("tag repository")
	}

	return &Service{
		Clock:        clk,
		Config:       cfg,
		IDs:          ids,
		Accounts:     accounts,
		Categories:   categories,
		Transactions: transactions,
		Tags:         tags,
	}, nil
}

// missingDependency reports a nil constructor argument as a registry error
// rather than a bare fmt.Errorf, so that a mis-wired binary fails the same
// way every other app-layer failure does — with a code a surface can turn
// into an exit code or a status (ADR-0011).
//
// The code is Internal because a nil dependency can only come from
// cmd/bodger wiring the container wrongly; nothing a user typed can cause
// it, and telling them which of bodger's internal parts is missing would
// be an internal detail they can't act on. So the dependency's name goes
// into the wrapped cause — logged in full, never serialised — and the user
// gets a message that says what happened and where to look. This is also
// the shape internal/lint's bare-error check exists to require: a
// fmt.Errorf inside Wrap is exactly right, the same call returned directly
// is not.
func missingDependency(name string) error {
	return errs.New(errs.Internal).
		Explain("bodger couldn't start because it isn't set up correctly. Check the log for what went wrong.").
		Wrap(fmt.Errorf("app: %s must not be nil", name))
}
