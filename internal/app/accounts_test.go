package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/idgen"
	"github.com/anirudhgray/bodger/internal/ports"
)

const testActorID = "actor-1"

// newTestService builds a *app.Service wired to the in-memory fakes, a
// frozen clock, and a fixed timezone - issue #6's "done when": "use-case
// tests run against in-memory repositories with a frozen clock and a
// fixed timezone."
//
// The ID generator is real (random) UUIDs, not idgen.NewSequence's
// deterministic-but-non-UUID-shaped IDs: normalize.Ref only treats a
// *Ref command field as an ID lookup when it parses as a UUID (falling
// back to name matching otherwise, by design), so a test that resolves an
// account or category back by the ID CreateAccount/CreateCategory just
// returned needs that ID to actually look like the ones production's
// idgen.UUID generates.
func newTestService(t *testing.T, frozenAt time.Time, tz string) *app.Service {
	t.Helper()
	clk := clock.NewFrozen(frozenAt)
	cfg := config.Defaults
	cfg.UserTimezone = tz
	accounts := newMemAccounts()
	categories := newMemCategories()
	transactions := newMemTransactions(categories)
	// newMemUsersSeeded only seeds ports.SeededUserID, the identity issue
	// #55's auth use cases hardcode. Every account/category/transaction
	// use-case test in this package uses testActorID instead - a distinct,
	// arbitrary actor that predates issue #55 entirely - so it needs its
	// own row too, now that CreateAccount/RecordOutflow/RecordInflow read
	// the acting user's reporting currency (issue #132).
	users := newMemUsersSeeded()
	users.byID[testActorID] = ports.User{ID: testActorID}
	svc, err := app.NewService(
		clk, cfg, idgen.New(), accounts, categories, transactions, newMemTags(),
		users, newMemSessions(), newMemAPITokens(), newMemFxRates(accounts, transactions), newMemFxProvider(),
		newMemImportBatches(), newMemImportRecords(),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func wantErrCode(t *testing.T, err error, code errs.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("want an error with code %s, got nil", code)
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want a *errs.Error with code %s", err, code)
	}
	if e.Code != code {
		t.Errorf("err code = %s, want %s (message: %s)", e.Code, code, e.Message)
	}
}

func TestCreateAccount(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	inst := "HDFC Bank"
	result, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID:            testActorID,
		Name:               "HDFC Savings",
		Kind:               "bank",
		Currency:           "INR",
		OpeningBalance:     "1,500.00",
		OpeningBalanceDate: "2026-01-01",
		Institution:        inst,
		SortOrder:          1,
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if result.Account.Name() != "HDFC Savings" {
		t.Errorf("Name = %q, want %q", result.Account.Name(), "HDFC Savings")
	}
	if result.Account.OpeningBalance().AmountMinor() != 150000 {
		t.Errorf("OpeningBalance = %d, want 150000", result.Account.OpeningBalance().AmountMinor())
	}
	if got, ok := result.Account.Institution(); !ok || got != inst {
		t.Errorf("Institution = %q, %v, want %q, true", got, ok, inst)
	}
	if _, ok := result.Account.OpeningBalanceDate(); !ok {
		t.Error("OpeningBalanceDate not set")
	}
	if result.Account.Archived() {
		t.Error("a freshly created account should not be archived")
	}
}

func TestCreateAccount_DefaultsOpeningBalanceToZeroAndNoDate(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	result, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID:  testActorID,
		Name:     "Cash Wallet",
		Kind:     "cash",
		Currency: "USD",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if !result.Account.OpeningBalance().IsZero() {
		t.Errorf("OpeningBalance = %v, want zero", result.Account.OpeningBalance())
	}
	if _, ok := result.Account.OpeningBalanceDate(); ok {
		t.Error("OpeningBalanceDate should be unset (nil) when not supplied, not defaulted to today")
	}
}

// TestCreateAccount_CurrencyPrecedence_FallsBackToInstanceDefaultWhenReportingCurrencyUnset
// is issue #132's explicit "no behaviour change for a fresh install"
// guarantee: an actor who has never set a reporting currency (every actor,
// before this issue existed) must still resolve CreateAccount's empty
// Currency field to the instance default, exactly as before the "user"
// rung of ADR-0004's ladder was wired in.
func TestCreateAccount_CurrencyPrecedence_FallsBackToInstanceDefaultWhenReportingCurrencyUnset(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	result, err := svc.CreateAccount(context.Background(), app.CreateAccountCommand{
		ActorID: testActorID, Name: "Cash", Kind: "cash",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if result.Account.Currency() != svc.Config.DefaultCurrency {
		t.Errorf("Currency = %s, want %s (the instance default, since neither entry nor user has an opinion)", result.Account.Currency(), svc.Config.DefaultCurrency)
	}
}

// TestCreateAccount_CurrencyPrecedence_UsesReportingCurrencyWhenSet proves
// the "user" rung is actually wired in: once an actor has a reporting
// currency, CreateAccount's empty Currency field resolves to it rather
// than falling all the way through to the instance default.
func TestCreateAccount_CurrencyPrecedence_UsesReportingCurrencyWhenSet(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	if err := svc.SetReportingCurrency(ctx, testActorID, "GBP"); err != nil {
		t.Fatalf("SetReportingCurrency: %v", err)
	}

	result, err := svc.CreateAccount(ctx, app.CreateAccountCommand{
		ActorID: testActorID, Name: "Cash", Kind: "cash",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if result.Account.Currency() != "GBP" {
		t.Errorf("Currency = %s, want GBP (the actor's reporting currency)", result.Account.Currency())
	}
}

// TestCreateAccount_UnrecognizedActor_StillResolvesCurrency covers a
// pre-existing quirk CreateAccount's currency-precedence wiring must not
// break: CreateAccount has never checked that its ActorID names a real
// row in the users table (there's no such check anywhere in this method),
// so an actor string that doesn't correspond to any user - as several
// tests in this file use for cross-actor-invisibility checks - must still
// resolve a currency (falling straight to the instance default, since it
// has no reporting currency to speak of) rather than failing with a
// not-found error the resolved-currency lookup introduced.
func TestCreateAccount_UnrecognizedActor_StillResolvesCurrency(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	result, err := svc.CreateAccount(context.Background(), app.CreateAccountCommand{
		ActorID: "someone-else", Name: "Not Mine", Kind: "bank",
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if result.Account.Currency() != svc.Config.DefaultCurrency {
		t.Errorf("Currency = %s, want %s", result.Account.Currency(), svc.Config.DefaultCurrency)
	}
}

func TestCreateAccount_InvalidKind(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.CreateAccount(context.Background(), app.CreateAccountCommand{
		ActorID:  testActorID,
		Name:     "Mystery",
		Kind:     "space-money",
		Currency: "USD",
	})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCreateAccount_RejectsDuplicateName(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()
	cmd := app.CreateAccountCommand{ActorID: testActorID, Name: "HDFC Savings", Kind: "bank", Currency: "INR"}
	if _, err := svc.CreateAccount(ctx, cmd); err != nil {
		t.Fatalf("first CreateAccount: %v", err)
	}
	_, err := svc.CreateAccount(ctx, cmd)
	wantErrCode(t, err, errs.Conflict)
}

func TestRenameAccount(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	created, err := svc.CreateAccount(ctx, app.CreateAccountCommand{ActorID: testActorID, Name: "Old Name", Kind: "bank", Currency: "USD"})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	renamed, err := svc.RenameAccount(ctx, app.RenameAccountCommand{
		ActorID:    testActorID,
		AccountRef: created.Account.ID(),
		Name:       "New Name",
	})
	if err != nil {
		t.Fatalf("RenameAccount: %v", err)
	}
	if renamed.Account.Name() != "New Name" {
		t.Errorf("Name = %q, want %q", renamed.Account.Name(), "New Name")
	}
	if renamed.Account.Kind() != created.Account.Kind() {
		t.Error("RenameAccount must not change Kind")
	}

	// Resolving by name (not just ID) must also work, and must reflect the
	// rename that already happened.
	again, err := svc.RenameAccount(ctx, app.RenameAccountCommand{ActorID: testActorID, AccountRef: "New Name", Name: "Newer Name"})
	if err != nil {
		t.Fatalf("RenameAccount by name: %v", err)
	}
	if again.Account.Name() != "Newer Name" {
		t.Errorf("Name = %q, want %q", again.Account.Name(), "Newer Name")
	}
}

func TestRenameAccount_UnknownRef(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.RenameAccount(context.Background(), app.RenameAccountCommand{ActorID: testActorID, AccountRef: "does-not-exist", Name: "New Name"})
	wantErrCode(t, err, errs.NotFound)
}

func TestRenameAccount_CrossActorRefIsInvisible(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	created, err := svc.CreateAccount(ctx, app.CreateAccountCommand{ActorID: testActorID, Name: "Mine", Kind: "bank", Currency: "USD"})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	// A different actor referencing this account's real ID must not be
	// able to touch it - ADR-0006's "the app layer makes its own
	// authorisation decision" (candidates are only ever the caller's own
	// List results, so a foreign ID can never resolve).
	_, err = svc.RenameAccount(ctx, app.RenameAccountCommand{ActorID: "someone-else", AccountRef: created.Account.ID(), Name: "Stolen"})
	wantErrCode(t, err, errs.NotFound)
}

func TestSetOpeningBalance(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	created, err := svc.CreateAccount(ctx, app.CreateAccountCommand{ActorID: testActorID, Name: "Savings", Kind: "bank", Currency: "INR"})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	updated, err := svc.SetOpeningBalance(ctx, app.SetOpeningBalanceCommand{
		ActorID:            testActorID,
		AccountRef:         created.Account.ID(),
		OpeningBalance:     "50000.00",
		OpeningBalanceDate: "2026-01-01",
	})
	if err != nil {
		t.Fatalf("SetOpeningBalance: %v", err)
	}
	if updated.Account.OpeningBalance().AmountMinor() != 5000000 {
		t.Errorf("OpeningBalance = %d, want 5000000", updated.Account.OpeningBalance().AmountMinor())
	}
	if d, ok := updated.Account.OpeningBalanceDate(); !ok || d.String() != "2026-01-01" {
		t.Errorf("OpeningBalanceDate = %v, %v, want 2026-01-01, true", d, ok)
	}
}

func TestArchiveAccount(t *testing.T) {
	frozen := time.Date(2026, time.August, 14, 10, 0, 0, 0, time.UTC)
	svc := newTestService(t, frozen, "UTC")
	ctx := context.Background()

	created, err := svc.CreateAccount(ctx, app.CreateAccountCommand{ActorID: testActorID, Name: "Old Account", Kind: "bank", Currency: "USD"})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	archived, err := svc.ArchiveAccount(ctx, app.ArchiveAccountCommand{ActorID: testActorID, AccountRef: created.Account.ID()})
	if err != nil {
		t.Fatalf("ArchiveAccount: %v", err)
	}
	if !archived.Account.Archived() {
		t.Fatal("account should be archived")
	}
	d, ok := archived.Account.ArchivedAt()
	if !ok || d.String() != "2026-08-14" {
		t.Errorf("ArchivedAt = %v, %v, want 2026-08-14, true", d, ok)
	}

	// Archiving again is idempotent: same archived date, no error.
	archivedAgain, err := svc.ArchiveAccount(ctx, app.ArchiveAccountCommand{ActorID: testActorID, AccountRef: created.Account.ID()})
	if err != nil {
		t.Fatalf("ArchiveAccount (again): %v", err)
	}
	d2, _ := archivedAgain.Account.ArchivedAt()
	if !d2.Equal(d) {
		t.Errorf("re-archiving changed ArchivedAt from %v to %v", d, d2)
	}
}

func TestListAccounts(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	if _, err := svc.CreateAccount(ctx, app.CreateAccountCommand{ActorID: testActorID, Name: "B Account", Kind: "bank", Currency: "USD"}); err != nil {
		t.Fatalf("CreateAccount B: %v", err)
	}
	if _, err := svc.CreateAccount(ctx, app.CreateAccountCommand{ActorID: testActorID, Name: "A Account", Kind: "bank", Currency: "USD"}); err != nil {
		t.Fatalf("CreateAccount A: %v", err)
	}
	if _, err := svc.CreateAccount(ctx, app.CreateAccountCommand{ActorID: "someone-else", Name: "Not Mine", Kind: "bank", Currency: "USD"}); err != nil {
		t.Fatalf("CreateAccount (other actor): %v", err)
	}

	result, err := svc.ListAccounts(ctx, app.ListAccountsQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(result.Accounts) != 2 {
		t.Fatalf("len(Accounts) = %d, want 2", len(result.Accounts))
	}
	if result.Accounts[0].Name() != "A Account" || result.Accounts[1].Name() != "B Account" {
		t.Errorf("Accounts = %+v, want A Account then B Account", result.Accounts)
	}
}

func TestAccountUseCases_RequireActorID(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.CreateAccount(ctx, app.CreateAccountCommand{Name: "No Actor", Kind: "bank", Currency: "USD"})
	wantErrCode(t, err, errs.InvalidInput)

	_, err = svc.ListAccounts(ctx, app.ListAccountsQuery{})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestGetAccount(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	created, err := svc.CreateAccount(ctx, app.CreateAccountCommand{ActorID: testActorID, Name: "HDFC Savings", Kind: "bank", Currency: "INR"})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	byID, err := svc.GetAccount(ctx, app.GetAccountQuery{ActorID: testActorID, AccountRef: created.Account.ID()})
	if err != nil {
		t.Fatalf("GetAccount by ID: %v", err)
	}
	if byID.Account.ID() != created.Account.ID() {
		t.Errorf("GetAccount by ID = %+v, want %+v", byID.Account, created.Account)
	}

	byName, err := svc.GetAccount(ctx, app.GetAccountQuery{ActorID: testActorID, AccountRef: "HDFC Savings"})
	if err != nil {
		t.Fatalf("GetAccount by name: %v", err)
	}
	if byName.Account.ID() != created.Account.ID() {
		t.Errorf("GetAccount by name = %+v, want %+v", byName.Account, created.Account)
	}
}

func TestGetAccount_UnknownRef(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.GetAccount(ctx, app.GetAccountQuery{ActorID: testActorID, AccountRef: "nonexistent"})
	wantErrCode(t, err, errs.NotFound)
}

func TestGetAccount_CrossActorRefIsInvisible(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	created, err := svc.CreateAccount(ctx, app.CreateAccountCommand{ActorID: "someone-else", Name: "Not Mine", Kind: "bank", Currency: "USD"})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	_, err = svc.GetAccount(ctx, app.GetAccountQuery{ActorID: testActorID, AccountRef: created.Account.ID()})
	wantErrCode(t, err, errs.NotFound)
}
