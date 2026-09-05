package app

import (
	"context"
	"strings"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// preserveOptions carries forward every optional field EditTransaction's
// command struct doesn't expose (posted date, related-transaction link,
// and import provenance) from the transaction being edited, plus the
// freshly normalised notes. Without this, an edit would silently drop an
// imported transaction's provenance or a refund's link back to the
// original purchase — full-replacement semantics (see EditTransactionCommand's
// doc comment) apply to the fields the command actually exposes, not to
// fields it was never given a way to express.
func preserveOptions(existing ledger.Transaction, notes string) []ledger.TransactionOption {
	var opts []ledger.TransactionOption
	if notes != "" {
		opts = append(opts, ledger.WithNotes(notes))
	}
	if d, ok := existing.PostedDate(); ok {
		opts = append(opts, ledger.WithPostedDate(d))
	}
	if related, ok := existing.RelatedTransactionID(); ok {
		opts = append(opts, ledger.WithRelatedTransaction(related))
	}
	importRecordID, hasImportRecord := existing.ImportRecordID()
	externalID, hasExternalID := existing.ExternalID()
	if hasImportRecord || hasExternalID {
		opts = append(opts, ledger.WithImportProvenance(importRecordID, externalID))
	}
	return opts
}

// EditTransactionCommand replaces a transaction's editable state. This is
// full-replacement, not a partial patch: the caller supplies the complete
// new state for every field this struct exposes, the same way
// RecordOutflow/RecordInflow/RecordTransfer do — there's no sentinel value
// meaning "leave this field alone", because ADR-0005's raw-string command
// fields already use the empty string to mean specific things (empty Date
// means "today", empty CategoryRef means "no category") that would
// conflict with also meaning "unchanged". A surface that wants to change
// only one field re-sends every other field's current value.
//
// The transaction's kind (outflow, inflow, or transfer) can't change — an
// outflow stays an outflow. AccountRef/Currency/CategoryRef apply when
// editing an outflow or inflow; FromAccountRef/ToAccountRef apply when
// editing a transfer. Whichever set doesn't match the existing
// transaction's kind is simply ignored.
type EditTransactionCommand struct {
	ActorID        string
	TransactionRef string

	// AccountRef, Currency, and CategoryRef apply only when the existing
	// transaction is an outflow or inflow.
	AccountRef  string
	Currency    string
	CategoryRef string

	// FromAccountRef and ToAccountRef apply only when the existing
	// transaction is a transfer.
	FromAccountRef string
	ToAccountRef   string

	Amount      string
	Date        string
	Description string
	Notes       string
	Tags        []string
}

// EditTransaction implements issue #6's "edit" use case. Every edit writes
// a transaction_revision row via the repository's Update (ADR-0002,
// data-model.md §7) — that happens inside
// internal/adapters/sqlite.TransactionRepository.Update, not here; this
// method's job is producing the correctly-reconstructed Transaction to
// hand it.
func (s *Service) EditTransaction(ctx context.Context, cmd EditTransactionCommand) (TransactionResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return TransactionResult{}, err
	}
	if strings.TrimSpace(cmd.TransactionRef) == "" {
		return TransactionResult{}, errs.New(errs.InvalidInput).Explain("A transaction ID is required.").Field("transaction_ref")
	}

	existing, _, err := s.Transactions.Get(ctx, cmd.ActorID, cmd.TransactionRef)
	if err != nil {
		return TransactionResult{}, err
	}

	fields, err := s.resolveCommonFields(cmd.Date, cmd.Description, cmd.Notes, cmd.Tags)
	if err != nil {
		return TransactionResult{}, err
	}
	opts := preserveOptions(existing, fields.Notes)

	var txn ledger.Transaction
	switch existing.Kind() {
	case ledger.TransactionKindOutflow, ledger.TransactionKindInflow:
		wantPositive := existing.Kind() == ledger.TransactionKindInflow
		posting, err := s.buildOutflowOrInflowPosting(ctx, cmd.ActorID, cmd.AccountRef, cmd.Amount, cmd.Currency, cmd.CategoryRef, wantPositive)
		if err != nil {
			return TransactionResult{}, err
		}
		if wantPositive {
			txn, err = ledger.NewInflow(existing.ID(), cmd.ActorID, fields.Date, fields.Description, []ledger.Posting{posting}, opts...)
		} else {
			txn, err = ledger.NewOutflow(existing.ID(), cmd.ActorID, fields.Date, fields.Description, []ledger.Posting{posting}, opts...)
		}
		if err != nil {
			return TransactionResult{}, wrapLedgerError(err)
		}

	case ledger.TransactionKindTransfer:
		outPosting, inPosting, err := s.buildTransferPostings(ctx, cmd.ActorID, cmd.FromAccountRef, cmd.ToAccountRef, cmd.Amount)
		if err != nil {
			return TransactionResult{}, err
		}
		// NewTransfer also returns the transfer's implied fx.Rate. It's
		// discarded here: RecordTransfer/EditTransaction don't persist it
		// yet (issue #133 wires fx_rate_used/fx_rate_source), and
		// buildTransferPostings already keeps the two accounts'
		// currencies equal, so this call site never hits the
		// cross-currency branch in practice.
		txn, _, err = ledger.NewTransfer(existing.ID(), cmd.ActorID, fields.Date, fields.Description, []ledger.Posting{outPosting, inPosting}, opts...)
		if err != nil {
			return TransactionResult{}, wrapTransferError(err)
		}

	default:
		return TransactionResult{}, errs.New(errs.Internal).Explain("Transaction %q has an unrecognized kind.", existing.ID())
	}

	if err := s.Transactions.Update(ctx, cmd.ActorID, txn, fields.Tags); err != nil {
		return TransactionResult{}, err
	}
	return TransactionResult{Transaction: txn, Tags: fields.Tags}, nil
}

// DeleteTransactionCommand soft-deletes a transaction (ADR-0002: deletion
// sets deleted_at, rows are never physically removed).
type DeleteTransactionCommand struct {
	ActorID        string
	TransactionRef string
}

// DeleteTransaction implements issue #6's "soft delete" use case. It calls
// Transactions.Get first — never Transactions.Update directly with a
// hand-rolled Transaction — so both the actor-ownership check and the
// "does this transaction even exist" check happen the normal way, and so
// the deleted copy carries forward every field (postings, tags, notes,
// provenance) unchanged except deletedAt. A soft-deleted transaction
// disappears from List and Get (the repository's own deleted_at IS NULL
// filter, issue #3's "done when") and therefore from AccountBalances too,
// while remaining present in the database.
func (s *Service) DeleteTransaction(ctx context.Context, cmd DeleteTransactionCommand) (TransactionResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return TransactionResult{}, err
	}
	if strings.TrimSpace(cmd.TransactionRef) == "" {
		return TransactionResult{}, errs.New(errs.InvalidInput).Explain("A transaction ID is required.").Field("transaction_ref")
	}

	existing, tags, err := s.Transactions.Get(ctx, cmd.ActorID, cmd.TransactionRef)
	if err != nil {
		return TransactionResult{}, err
	}

	deleted := existing.Delete(s.Clock.Now())
	if err := s.Transactions.Update(ctx, cmd.ActorID, deleted, tags); err != nil {
		return TransactionResult{}, err
	}
	return TransactionResult{Transaction: deleted, Tags: tags}, nil
}
