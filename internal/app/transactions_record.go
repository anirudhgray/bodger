package app

import (
	"context"
	"strings"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

const (
	maxDescriptionLen = 500
	maxNotesLen       = 4000
)

// TransactionResult wraps the Transaction (and its Tags) a record, edit,
// or delete use case produced or affected.
type TransactionResult struct {
	Transaction ledger.Transaction
	Tags        []ledger.Tag
}

// commonTransactionFields is the subset of a transaction's fields every
// kind shares — date, description, notes, tags — normalised once by
// resolveCommonFields and reused by RecordOutflow, RecordInflow,
// RecordTransfer, and EditTransaction, so the normalisation order and
// error handling for these fields lives in exactly one place.
type commonTransactionFields struct {
	Date        domain.Date
	Description string
	Notes       string
	Tags        []ledger.Tag
}

// resolveCommonFields normalises date, description, notes, and tags. date
// resolves through normalize.DateOf, so an empty string means "today" —
// unlike an account's opening-balance date, a transaction's booked date is
// not optional (data-model.md §5 requires one), so defaulting to today
// here is the correct behaviour, not the surprise optionalDate exists to
// avoid elsewhere.
func (s *Service) resolveCommonFields(date, description, notes string, rawTags []string) (commonTransactionFields, error) {
	d, err := normalize.DateOf(date, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return commonTransactionFields{}, err
	}

	desc, err := normalize.Text(description, maxDescriptionLen)
	if err != nil {
		return commonTransactionFields{}, attachField(err, "description")
	}
	if desc == "" {
		return commonTransactionFields{}, errs.New(errs.InvalidInput).Explain("A description is required.").Field("description")
	}

	notesNorm, err := normalize.Text(notes, maxNotesLen)
	if err != nil {
		return commonTransactionFields{}, attachField(err, "notes")
	}

	tags, err := normalizeTags(rawTags)
	if err != nil {
		return commonTransactionFields{}, err
	}

	return commonTransactionFields{Date: d, Description: desc, Notes: notesNorm, Tags: tags}, nil
}

// buildOutflowOrInflowPosting resolves accountRef, currency, amount, and
// (optionally) categoryRef into a single ledger.Posting, applying
// ADR-0005's normalisation order — account, then currency (which needs the
// account to read its default currency off), then amount (which needs the
// resolved currency's minor-unit exponent). wantPositive forces the
// posting's sign regardless of what the caller typed: an outflow amount is
// always negative and an inflow amount is always positive, so a user who
// types "-800" for an outflow gets the same posting as one who types
// "800".
//
// It's shared by RecordOutflow, RecordInflow, and EditTransaction (when
// editing an outflow or inflow) - the one place this resolution happens,
// so the three don't drift.
func (s *Service) buildOutflowOrInflowPosting(ctx context.Context, actorID, accountRef, amount, currency, categoryRef string, wantPositive bool) (ledger.Posting, error) {
	account, err := s.resolveOwnedAccount(ctx, actorID, accountRef)
	if err != nil {
		return ledger.Posting{}, attachField(err, "account_ref")
	}

	resolvedCurrency, err := normalize.Currency(currency, account.Currency(), "", s.Config.DefaultCurrency)
	if err != nil {
		return ledger.Posting{}, err
	}

	amountMinor, err := normalize.Amount(amount, resolvedCurrency)
	if err != nil {
		return ledger.Posting{}, err
	}
	amountMinor = signedAmount(amountMinor, wantPositive)
	if amountMinor == 0 {
		return ledger.Posting{}, errs.New(errs.InvalidInput).Explain("Amount must not be zero.").Field("amount")
	}

	var categoryIDPtr *string
	if strings.TrimSpace(categoryRef) != "" {
		category, err := s.resolveOwnedCategory(ctx, actorID, categoryRef)
		if err != nil {
			return ledger.Posting{}, attachField(err, "category_ref")
		}
		id := category.ID()
		categoryIDPtr = &id
	}

	m, err := money.NewMoney(amountMinor, resolvedCurrency)
	if err != nil {
		return ledger.Posting{}, errs.New(errs.Internal).Wrap(err)
	}

	posting, err := ledger.NewPosting(s.IDs.NewID(), account.ID(), m, categoryIDPtr, 0)
	if err != nil {
		return ledger.Posting{}, errs.New(errs.Internal).Wrap(err)
	}
	return posting, nil
}

// RecordOutflowCommand records a single-posting outflow: money leaving
// AccountRef, optionally attributed to CategoryRef. Split entry (n
// postings across several categories) is M2 scope, not built here - see
// issue #6's out-of-scope list.
type RecordOutflowCommand struct {
	ActorID     string
	AccountRef  string
	Amount      string
	Currency    string
	CategoryRef string
	Date        string
	Description string
	Notes       string
	Tags        []string
}

// RecordOutflow implements issue #6's RecordOutflow use case.
func (s *Service) RecordOutflow(ctx context.Context, cmd RecordOutflowCommand) (TransactionResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return TransactionResult{}, err
	}

	posting, err := s.buildOutflowOrInflowPosting(ctx, cmd.ActorID, cmd.AccountRef, cmd.Amount, cmd.Currency, cmd.CategoryRef, false)
	if err != nil {
		return TransactionResult{}, err
	}

	fields, err := s.resolveCommonFields(cmd.Date, cmd.Description, cmd.Notes, cmd.Tags)
	if err != nil {
		return TransactionResult{}, err
	}

	var opts []ledger.TransactionOption
	if fields.Notes != "" {
		opts = append(opts, ledger.WithNotes(fields.Notes))
	}

	txn, err := ledger.NewOutflow(s.IDs.NewID(), cmd.ActorID, fields.Date, fields.Description, []ledger.Posting{posting}, opts...)
	if err != nil {
		return TransactionResult{}, wrapLedgerError(err)
	}

	if err := s.Transactions.Create(ctx, cmd.ActorID, txn, fields.Tags); err != nil {
		return TransactionResult{}, err
	}
	return TransactionResult{Transaction: txn, Tags: fields.Tags}, nil
}

// buildTransferPostings resolves fromRef and toRef against the actor's own
// accounts and builds the two opposite-signed, uncategorised postings a
// transfer needs (data-model.md §5: "exactly two postings; exactly two
// distinct accounts; opposite signs; both categories null"). The amount's
// currency is always the *from* account's currency — there is no
// Currency command field for a transfer, unlike RecordOutflow/RecordInflow,
// because a transfer's amount isn't an independent fact the way an entry's
// amount is; it's a movement between two accounts that already have
// currencies. Building the *to* posting in the *to* account's own currency
// (rather than reusing the *from* currency) is what would let
// ledger.NewTransfer's own cross-currency handling (issue #129) take over
// if this function let a differing pair through.
//
// It doesn't, yet: ledger.NewTransfer now accepts and derives an implied
// rate for a cross-currency transfer rather than rejecting it, but nothing
// in this application-layer path persists that rate (fx_rate_used /
// fx_rate_source) or surfaces it to a caller — that's issue #133. Until
// #133 wires it up, this function keeps explicitly rejecting a
// cross-currency pair itself, so RecordTransfer's user-facing behaviour is
// unchanged: a transfer between differently-currencied accounts still
// fails with a clear, specific error, rather than silently succeeding with
// an implied rate nothing downstream ever records.
//
// Shared by RecordTransfer and EditTransaction (when editing a transfer).
func (s *Service) buildTransferPostings(ctx context.Context, actorID, fromRef, toRef, amount string) (ledger.Posting, ledger.Posting, error) {
	fromAccount, err := s.resolveOwnedAccount(ctx, actorID, fromRef)
	if err != nil {
		return ledger.Posting{}, ledger.Posting{}, attachField(err, "from_account_ref")
	}
	toAccount, err := s.resolveOwnedAccount(ctx, actorID, toRef)
	if err != nil {
		return ledger.Posting{}, ledger.Posting{}, attachField(err, "to_account_ref")
	}
	if fromAccount.Currency() != toAccount.Currency() {
		return ledger.Posting{}, ledger.Posting{}, errs.New(errs.InvalidInput).
			Explain("Transfers between accounts with different currencies aren't supported yet.").
			Field("to_account_ref")
	}

	amountMinor, err := normalize.Amount(amount, fromAccount.Currency())
	if err != nil {
		return ledger.Posting{}, ledger.Posting{}, err
	}
	if amountMinor < 0 {
		amountMinor = -amountMinor
	}
	if amountMinor == 0 {
		return ledger.Posting{}, ledger.Posting{}, errs.New(errs.InvalidInput).Explain("Amount must not be zero.").Field("amount")
	}

	fromMoney, err := money.NewMoney(-amountMinor, fromAccount.Currency())
	if err != nil {
		return ledger.Posting{}, ledger.Posting{}, errs.New(errs.Internal).Wrap(err)
	}
	toMoney, err := money.NewMoney(amountMinor, toAccount.Currency())
	if err != nil {
		return ledger.Posting{}, ledger.Posting{}, errs.New(errs.Internal).Wrap(err)
	}

	outPosting, err := ledger.NewPosting(s.IDs.NewID(), fromAccount.ID(), fromMoney, nil, 0)
	if err != nil {
		return ledger.Posting{}, ledger.Posting{}, errs.New(errs.Internal).Wrap(err)
	}
	inPosting, err := ledger.NewPosting(s.IDs.NewID(), toAccount.ID(), toMoney, nil, 1)
	if err != nil {
		return ledger.Posting{}, ledger.Posting{}, errs.New(errs.Internal).Wrap(err)
	}
	return outPosting, inPosting, nil
}

// RecordTransferCommand records a transfer of exactly Amount from
// FromAccountRef to ToAccountRef. A cross-currency transfer is rejected by
// buildTransferPostings — the domain layer (ledger.NewTransfer, issue #129)
// now supports one, but wiring the implied rate through this command is
// issue #133's job, so this command keeps rejecting it in the meantime.
type RecordTransferCommand struct {
	ActorID        string
	FromAccountRef string
	ToAccountRef   string
	Amount         string
	Date           string
	Description    string
	Notes          string
	Tags           []string
}

// RecordTransfer implements issue #6's RecordTransfer use case: exactly
// two postings, never split (data-model.md §13: "a transfer with more than
// two postings is rejected").
func (s *Service) RecordTransfer(ctx context.Context, cmd RecordTransferCommand) (TransactionResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return TransactionResult{}, err
	}

	outPosting, inPosting, err := s.buildTransferPostings(ctx, cmd.ActorID, cmd.FromAccountRef, cmd.ToAccountRef, cmd.Amount)
	if err != nil {
		return TransactionResult{}, err
	}

	fields, err := s.resolveCommonFields(cmd.Date, cmd.Description, cmd.Notes, cmd.Tags)
	if err != nil {
		return TransactionResult{}, err
	}

	var opts []ledger.TransactionOption
	if fields.Notes != "" {
		opts = append(opts, ledger.WithNotes(fields.Notes))
	}

	// NewTransfer also returns the transfer's implied fx.Rate. It's
	// discarded here: buildTransferPostings above already rejects a
	// cross-currency pair, so this call site only ever exercises the
	// same-currency (identity-rate) branch, and issue #133 is what will
	// eventually persist the rate for real.
	txn, _, err := ledger.NewTransfer(s.IDs.NewID(), cmd.ActorID, fields.Date, fields.Description, []ledger.Posting{outPosting, inPosting}, opts...)
	if err != nil {
		return TransactionResult{}, wrapTransferError(err)
	}

	if err := s.Transactions.Create(ctx, cmd.ActorID, txn, fields.Tags); err != nil {
		return TransactionResult{}, err
	}
	return TransactionResult{Transaction: txn, Tags: fields.Tags}, nil
}

// RecordInflowCommand records a single-posting inflow: money arriving in
// AccountRef, optionally attributed to CategoryRef. A refund is recorded
// this way too, with CategoryRef set to the original outflow's category
// (data-model.md §7) - there's no separate refund command, since an
// inflow's category is not constrained to income-kind categories (that
// constraint would make refunds impossible to express).
type RecordInflowCommand struct {
	ActorID     string
	AccountRef  string
	Amount      string
	Currency    string
	CategoryRef string
	Date        string
	Description string
	Notes       string
	Tags        []string
}

// RecordInflow implements issue #6's RecordInflow use case.
func (s *Service) RecordInflow(ctx context.Context, cmd RecordInflowCommand) (TransactionResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return TransactionResult{}, err
	}

	posting, err := s.buildOutflowOrInflowPosting(ctx, cmd.ActorID, cmd.AccountRef, cmd.Amount, cmd.Currency, cmd.CategoryRef, true)
	if err != nil {
		return TransactionResult{}, err
	}

	fields, err := s.resolveCommonFields(cmd.Date, cmd.Description, cmd.Notes, cmd.Tags)
	if err != nil {
		return TransactionResult{}, err
	}

	var opts []ledger.TransactionOption
	if fields.Notes != "" {
		opts = append(opts, ledger.WithNotes(fields.Notes))
	}

	txn, err := ledger.NewInflow(s.IDs.NewID(), cmd.ActorID, fields.Date, fields.Description, []ledger.Posting{posting}, opts...)
	if err != nil {
		return TransactionResult{}, wrapLedgerError(err)
	}

	if err := s.Transactions.Create(ctx, cmd.ActorID, txn, fields.Tags); err != nil {
		return TransactionResult{}, err
	}
	return TransactionResult{Transaction: txn, Tags: fields.Tags}, nil
}
