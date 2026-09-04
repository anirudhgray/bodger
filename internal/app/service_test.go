package app_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	"github.com/anirudhgray/bodger/internal/ports"
)

// fakeAccounts, fakeCategories, fakeTransactions, and fakeTags are
// minimal stand-ins for the real internal/adapters/sqlite repositories —
// enough to satisfy the ports interfaces for NewService's wiring tests.
// They do no actual persistence; app-layer use-case tests (issue #6) will
// need fuller in-memory fakes, but constructing a Service doesn't.
type fakeAccounts struct{}

func (fakeAccounts) Create(context.Context, string, ledger.Account) error { return nil }
func (fakeAccounts) Get(context.Context, string, string) (ledger.Account, error) {
	return ledger.Account{}, nil
}
func (fakeAccounts) List(context.Context, string) ([]ledger.Account, error) { return nil, nil }
func (fakeAccounts) Update(context.Context, string, ledger.Account) error   { return nil }

type fakeCategories struct{}

func (fakeCategories) Create(context.Context, string, ledger.Category) error { return nil }
func (fakeCategories) Get(context.Context, string, string) (ledger.Category, error) {
	return ledger.Category{}, nil
}
func (fakeCategories) List(context.Context, string) ([]ledger.Category, error) { return nil, nil }
func (fakeCategories) Update(context.Context, string, ledger.Category) error   { return nil }

type fakeTransactions struct{}

func (fakeTransactions) Create(context.Context, string, ledger.Transaction, []ledger.Tag) error {
	return nil
}
func (fakeTransactions) Get(context.Context, string, string) (ledger.Transaction, []ledger.Tag, error) {
	return ledger.Transaction{}, nil, nil
}
func (fakeTransactions) List(context.Context, string, ports.TransactionFilter) ([]ledger.Transaction, error) {
	return nil, nil
}
func (fakeTransactions) Update(context.Context, string, ledger.Transaction, []ledger.Tag) error {
	return nil
}

type fakeTags struct{}

func (fakeTags) List(context.Context, string) ([]ledger.Tag, error) { return nil, nil }

type fakeUsers struct{}

func (fakeUsers) GetByID(context.Context, string) (ports.User, error)   { return ports.User{}, nil }
func (fakeUsers) SetPasswordHash(context.Context, string, string) error { return nil }

type fakeSessions struct{}

func (fakeSessions) Create(context.Context, string, ports.Session) error { return nil }
func (fakeSessions) GetByTokenHash(context.Context, string) (ports.Session, error) {
	return ports.Session{}, nil
}
func (fakeSessions) Touch(context.Context, string, string, time.Time, time.Time) error { return nil }
func (fakeSessions) Delete(context.Context, string, string) error                      { return nil }
func (fakeSessions) ListByUser(context.Context, string) ([]ports.Session, error)       { return nil, nil }
func (fakeSessions) DeleteAllByUser(context.Context, string) error                     { return nil }

type fakeAPITokens struct{}

func (fakeAPITokens) Create(context.Context, string, ports.APIToken) error { return nil }
func (fakeAPITokens) GetByTokenHash(context.Context, string) (ports.APIToken, error) {
	return ports.APIToken{}, nil
}
func (fakeAPITokens) List(context.Context, string) ([]ports.APIToken, error)  { return nil, nil }
func (fakeAPITokens) Touch(context.Context, string, string, time.Time) error  { return nil }
func (fakeAPITokens) Revoke(context.Context, string, string, time.Time) error { return nil }

// TestNewService_RejectsMissingDependencies checks both halves of
// ADR-0011's safe/internal split for a mis-wired container: the caller gets
// an *errs.Error coded Internal (so a surface has an exit code and a
// status), and the name of the dependency that was nil reaches the log
// rather than the message. wantCause is therefore asserted against the
// logged cause chain, not against err.Error() — the same detail as before,
// on the side of the split that is allowed to carry it.
func TestNewService_RejectsMissingDependencies(t *testing.T) {
	clk := clock.NewFrozen(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.Defaults
	ids := idgen.NewSequence("test")

	tests := []struct {
		name         string
		clk          clock.Clock
		ids          idgen.Generator
		accounts     ports.AccountRepository
		categories   ports.CategoryRepository
		transactions ports.TransactionRepository
		tags         ports.TagRepository
		users        ports.UserRepository
		sessions     ports.SessionRepository
		apiTokens    ports.APITokenRepository
		wantCause    string
	}{
		{
			name:         "nil clock",
			clk:          nil,
			ids:          ids,
			accounts:     fakeAccounts{},
			categories:   fakeCategories{},
			transactions: fakeTransactions{},
			tags:         fakeTags{},
			users:        fakeUsers{},
			sessions:     fakeSessions{},
			apiTokens:    fakeAPITokens{},
			wantCause:    "clock",
		},
		{
			name:         "nil id generator",
			clk:          clk,
			ids:          nil,
			accounts:     fakeAccounts{},
			categories:   fakeCategories{},
			transactions: fakeTransactions{},
			tags:         fakeTags{},
			users:        fakeUsers{},
			sessions:     fakeSessions{},
			apiTokens:    fakeAPITokens{},
			wantCause:    "id generator",
		},
		{
			name:         "nil account repository",
			clk:          clk,
			ids:          ids,
			accounts:     nil,
			categories:   fakeCategories{},
			transactions: fakeTransactions{},
			tags:         fakeTags{},
			users:        fakeUsers{},
			sessions:     fakeSessions{},
			apiTokens:    fakeAPITokens{},
			wantCause:    "account repository",
		},
		{
			name:         "nil category repository",
			clk:          clk,
			ids:          ids,
			accounts:     fakeAccounts{},
			categories:   nil,
			transactions: fakeTransactions{},
			tags:         fakeTags{},
			users:        fakeUsers{},
			sessions:     fakeSessions{},
			apiTokens:    fakeAPITokens{},
			wantCause:    "category repository",
		},
		{
			name:         "nil transaction repository",
			clk:          clk,
			ids:          ids,
			accounts:     fakeAccounts{},
			categories:   fakeCategories{},
			transactions: nil,
			tags:         fakeTags{},
			users:        fakeUsers{},
			sessions:     fakeSessions{},
			apiTokens:    fakeAPITokens{},
			wantCause:    "transaction repository",
		},
		{
			name:         "nil tag repository",
			clk:          clk,
			ids:          ids,
			accounts:     fakeAccounts{},
			categories:   fakeCategories{},
			transactions: fakeTransactions{},
			tags:         nil,
			users:        fakeUsers{},
			sessions:     fakeSessions{},
			apiTokens:    fakeAPITokens{},
			wantCause:    "tag repository",
		},
		{
			name:         "nil user repository",
			clk:          clk,
			ids:          ids,
			accounts:     fakeAccounts{},
			categories:   fakeCategories{},
			transactions: fakeTransactions{},
			tags:         fakeTags{},
			users:        nil,
			sessions:     fakeSessions{},
			apiTokens:    fakeAPITokens{},
			wantCause:    "user repository",
		},
		{
			name:         "nil session repository",
			clk:          clk,
			ids:          ids,
			accounts:     fakeAccounts{},
			categories:   fakeCategories{},
			transactions: fakeTransactions{},
			tags:         fakeTags{},
			users:        fakeUsers{},
			sessions:     nil,
			apiTokens:    fakeAPITokens{},
			wantCause:    "session repository",
		},
		{
			name:         "nil API token repository",
			clk:          clk,
			ids:          ids,
			accounts:     fakeAccounts{},
			categories:   fakeCategories{},
			transactions: fakeTransactions{},
			tags:         fakeTags{},
			users:        fakeUsers{},
			sessions:     fakeSessions{},
			apiTokens:    nil,
			wantCause:    "API token repository",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := app.NewService(
				tt.clk, cfg, tt.ids, tt.accounts, tt.categories, tt.transactions, tt.tags,
				tt.users, tt.sessions, tt.apiTokens,
			)
			if err == nil {
				t.Fatalf("NewService(...) returned no error, want one caused by a nil %s", tt.wantCause)
			}

			var e *errs.Error
			if !errors.As(err, &e) {
				t.Fatalf("NewService(...) error = %T (%v), want an *errs.Error", err, err)
			}
			if e.Code != errs.Internal {
				t.Errorf("NewService(...) error code = %q, want %q — a nil dependency is a wiring bug, not the user's fault", e.Code, errs.Internal)
			}
			if strings.Contains(e.CLIMessage(), tt.wantCause) {
				t.Errorf("NewService(...) user-facing message names the internal dependency %q: %q", tt.wantCause, e.CLIMessage())
			}

			var buf bytes.Buffer
			slog.New(slog.NewJSONHandler(&buf, nil)).Error("service construction failed", "error", err)
			if !strings.Contains(buf.String(), tt.wantCause) {
				t.Errorf("log record is missing the cause detail %q\ngot: %s", tt.wantCause, buf.String())
			}
		})
	}
}

func TestNewService_BuildsWithEveryDependency(t *testing.T) {
	clk := clock.NewFrozen(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	cfg := config.Defaults
	ids := idgen.NewSequence("test")

	svc, err := app.NewService(
		clk, cfg, ids, fakeAccounts{}, fakeCategories{}, fakeTransactions{}, fakeTags{},
		fakeUsers{}, fakeSessions{}, fakeAPITokens{},
	)
	if err != nil {
		t.Fatalf("NewService(...) unexpected error: %v", err)
	}
	if svc.Clock != clock.Clock(clk) {
		t.Error("Service.Clock does not match the clock passed to NewService")
	}
	if svc.Config != cfg {
		t.Error("Service.Config does not match the config passed to NewService")
	}
	if svc.IDs != idgen.Generator(ids) {
		t.Error("Service.IDs does not match the id generator passed to NewService")
	}
	if svc.Accounts == nil || svc.Categories == nil || svc.Transactions == nil || svc.Tags == nil {
		t.Error("Service repositories should all be non-nil after a successful NewService call")
	}
	if svc.Users == nil || svc.Sessions == nil || svc.APITokens == nil {
		t.Error("Service auth repositories should all be non-nil after a successful NewService call")
	}
}

// TestService_UsesInjectedClockNotWallClock is a light guard that Service
// holds whatever Clock it was given rather than silently substituting the
// real one — the concrete assertion that "no function in this package
// calls time.Now()" belongs to the banned-symbol check (issue #9), but
// this at least proves the container's own wiring doesn't bypass it.
func TestService_UsesInjectedClockNotWallClock(t *testing.T) {
	frozenAt := time.Date(2026, time.July, 31, 18, 45, 0, 0, time.UTC)
	clk := clock.NewFrozen(frozenAt)

	svc, err := app.NewService(
		clk, config.Defaults, idgen.NewSequence("t"), fakeAccounts{}, fakeCategories{}, fakeTransactions{}, fakeTags{},
		fakeUsers{}, fakeSessions{}, fakeAPITokens{},
	)
	if err != nil {
		t.Fatalf("NewService(...) unexpected error: %v", err)
	}
	if got := svc.Clock.Now(); !got.Equal(frozenAt) {
		t.Errorf("Service.Clock.Now() = %s, want the frozen instant %s", got, frozenAt)
	}
}
