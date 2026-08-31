package app

import (
	"context"
	"strings"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

const (
	maxAccountNameLen = 200
	maxInstitutionLen = 200
)

// AccountResult wraps the Account a create/rename/set-opening-balance/
// archive use case produced or affected.
type AccountResult struct {
	Account ledger.Account
}

// CreateAccountCommand creates a new account. Name and Institution are
// normalised via normalize.Text; Kind is validated against ledger's closed
// set; Currency walks ADR-0004's precedence ladder (entry, here, ->
// instance default - there is no "account" rung yet, since the account
// being created is what a future entry's ladder would resolve against, and
// no "user" rung yet, since M1 has no per-user reporting currency separate
// from the instance default). OpeningBalance defaults to zero when empty;
// OpeningBalanceDate is nil (no declared date) when empty, per
// optionalDate's doc comment - this is deliberately not the same as
// resolving to "today".
type CreateAccountCommand struct {
	ActorID            string
	Name               string
	Kind               string
	Currency           string
	OpeningBalance     string
	OpeningBalanceDate string
	Institution        string
	SortOrder          int
}

// CreateAccount implements the "create" use case in issue #6's accounts
// scope.
func (s *Service) CreateAccount(ctx context.Context, cmd CreateAccountCommand) (AccountResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return AccountResult{}, err
	}

	name, err := normalize.Text(cmd.Name, maxAccountNameLen)
	if err != nil {
		return AccountResult{}, attachField(err, "name")
	}
	if name == "" {
		return AccountResult{}, errs.New(errs.InvalidInput).Explain("A name is required.").Field("name")
	}

	kind, err := parseAccountKind(cmd.Kind)
	if err != nil {
		return AccountResult{}, err
	}

	currency, err := normalize.Currency(cmd.Currency, "", "", s.Config.DefaultCurrency)
	if err != nil {
		return AccountResult{}, err
	}

	var openingMinor int64
	if strings.TrimSpace(cmd.OpeningBalance) != "" {
		openingMinor, err = normalize.Amount(cmd.OpeningBalance, currency)
		if err != nil {
			return AccountResult{}, err
		}
	}
	openingMoney, err := money.NewMoney(openingMinor, currency)
	if err != nil {
		return AccountResult{}, errs.New(errs.Internal).Wrap(err)
	}

	obDatePtr, err := optionalDate(cmd.OpeningBalanceDate, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return AccountResult{}, err
	}

	institutionPtr, err := optionalText(cmd.Institution, maxInstitutionLen, "institution")
	if err != nil {
		return AccountResult{}, err
	}

	account, err := ledger.NewAccount(s.IDs.NewID(), cmd.ActorID, name, kind, openingMoney, obDatePtr, institutionPtr, cmd.SortOrder, nil)
	if err != nil {
		return AccountResult{}, wrapLedgerError(err)
	}

	if err := s.Accounts.Create(ctx, cmd.ActorID, account); err != nil {
		return AccountResult{}, err
	}
	return AccountResult{Account: account}, nil
}

// RenameAccountCommand renames an existing account, leaving every other
// field untouched.
type RenameAccountCommand struct {
	ActorID    string
	AccountRef string
	Name       string
}

// RenameAccount implements the "rename" use case in issue #6's accounts
// scope.
func (s *Service) RenameAccount(ctx context.Context, cmd RenameAccountCommand) (AccountResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return AccountResult{}, err
	}
	existing, err := s.resolveOwnedAccount(ctx, cmd.ActorID, cmd.AccountRef)
	if err != nil {
		return AccountResult{}, attachField(err, "account_ref")
	}

	name, err := normalize.Text(cmd.Name, maxAccountNameLen)
	if err != nil {
		return AccountResult{}, attachField(err, "name")
	}
	if name == "" {
		return AccountResult{}, errs.New(errs.InvalidInput).Explain("A name is required.").Field("name")
	}

	updated, err := ledger.NewAccount(
		existing.ID(), existing.UserID(), name, existing.Kind(), existing.OpeningBalance(),
		accountOpeningBalanceDatePtr(existing), accountInstitutionPtr(existing), existing.SortOrder(), accountArchivedAtPtr(existing),
	)
	if err != nil {
		return AccountResult{}, wrapLedgerError(err)
	}

	if err := s.Accounts.Update(ctx, cmd.ActorID, updated); err != nil {
		return AccountResult{}, err
	}
	return AccountResult{Account: updated}, nil
}

// SetOpeningBalanceCommand re-declares an account's starting balance -
// data-model.md §4's "declared starting point, not a cached aggregate": it
// never changes as transactions arrive, only when the user explicitly
// asserts a new one here.
type SetOpeningBalanceCommand struct {
	ActorID            string
	AccountRef         string
	OpeningBalance     string
	OpeningBalanceDate string
}

// SetOpeningBalance implements the "set opening balance" use case in issue
// #6's accounts scope. The new balance is always in the account's own
// currency - an account's currency is fixed at creation (data-model.md §4
// doesn't model changing it, and nothing in this issue's scope asks for
// that), so there's no currency precedence to walk here.
func (s *Service) SetOpeningBalance(ctx context.Context, cmd SetOpeningBalanceCommand) (AccountResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return AccountResult{}, err
	}
	existing, err := s.resolveOwnedAccount(ctx, cmd.ActorID, cmd.AccountRef)
	if err != nil {
		return AccountResult{}, attachField(err, "account_ref")
	}

	amountMinor, err := normalize.Amount(cmd.OpeningBalance, existing.Currency())
	if err != nil {
		return AccountResult{}, err
	}
	openingMoney, err := money.NewMoney(amountMinor, existing.Currency())
	if err != nil {
		return AccountResult{}, errs.New(errs.Internal).Wrap(err)
	}

	obDatePtr, err := optionalDate(cmd.OpeningBalanceDate, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return AccountResult{}, err
	}

	updated, err := ledger.NewAccount(
		existing.ID(), existing.UserID(), existing.Name(), existing.Kind(), openingMoney,
		obDatePtr, accountInstitutionPtr(existing), existing.SortOrder(), accountArchivedAtPtr(existing),
	)
	if err != nil {
		return AccountResult{}, wrapLedgerError(err)
	}

	if err := s.Accounts.Update(ctx, cmd.ActorID, updated); err != nil {
		return AccountResult{}, err
	}
	return AccountResult{Account: updated}, nil
}

// ArchiveAccountCommand hides an account from pickers while leaving its
// history in place (data-model.md §4). Archiving an already-archived
// account is a no-op that returns its current state unchanged, rather than
// an error or a silently overwritten archive date - a script that retries
// an archive call shouldn't fail, and re-stamping today's date over the
// account's real archive date would lose information nobody asked to
// change.
type ArchiveAccountCommand struct {
	ActorID    string
	AccountRef string
}

// ArchiveAccount implements the "archive" use case in issue #6's accounts
// scope.
func (s *Service) ArchiveAccount(ctx context.Context, cmd ArchiveAccountCommand) (AccountResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return AccountResult{}, err
	}
	existing, err := s.resolveOwnedAccount(ctx, cmd.ActorID, cmd.AccountRef)
	if err != nil {
		return AccountResult{}, attachField(err, "account_ref")
	}
	if existing.Archived() {
		return AccountResult{Account: existing}, nil
	}

	today, err := normalize.DateOf("", s.Clock, s.Config.UserTimezone)
	if err != nil {
		return AccountResult{}, err
	}

	updated, err := ledger.NewAccount(
		existing.ID(), existing.UserID(), existing.Name(), existing.Kind(), existing.OpeningBalance(),
		accountOpeningBalanceDatePtr(existing), accountInstitutionPtr(existing), existing.SortOrder(), &today,
	)
	if err != nil {
		return AccountResult{}, wrapLedgerError(err)
	}

	if err := s.Accounts.Update(ctx, cmd.ActorID, updated); err != nil {
		return AccountResult{}, err
	}
	return AccountResult{Account: updated}, nil
}

// ListAccountsQuery lists every account actorID owns.
type ListAccountsQuery struct {
	ActorID string
}

// ListAccountsResult is ListAccounts' result, in the repository's name
// order.
type ListAccountsResult struct {
	Accounts []ledger.Account
}

// ListAccounts implements the "list" use case in issue #6's accounts
// scope.
func (s *Service) ListAccounts(ctx context.Context, q ListAccountsQuery) (ListAccountsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ListAccountsResult{}, err
	}
	accounts, err := s.Accounts.List(ctx, q.ActorID)
	if err != nil {
		return ListAccountsResult{}, err
	}
	return ListAccountsResult{Accounts: accounts}, nil
}
