package app

import (
	"errors"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// attachField gives err a FieldPath, for the normalize functions (Ref, in
// particular) that don't know which command field they were resolving on
// behalf of and so return an *errs.Error with no field set — see
// normalize.Text's doc comment for the same pattern. If err doesn't wrap an
// *errs.Error, it's returned unchanged: this is a best-effort annotation,
// never a requirement for err to already be one of ours.
func attachField(err error, field string) error {
	if err == nil {
		return nil
	}
	var e *errs.Error
	if errors.As(err, &e) {
		e.Field(field)
	}
	return err
}

// wrapLedgerError turns an error returned by an internal/domain/ledger
// constructor into an *errs.Error a surface can render. By the time this
// package calls a ledger constructor, every field has already been
// normalised and every reference has already been resolved against the
// actor's own data — so a ledger constructor failing here means a business
// rule was violated (e.g. an amount that normalised to exactly zero), not
// that the input was unparseable. InvalidInput is therefore the right
// default; it is never Internal, because nothing about a validation
// failure here is a bug.
//
// If err already wraps an *errs.Error (defensive: none of this package's
// own pre-checks currently produce one that reaches here, but a future one
// might), that error is returned as-is rather than double-wrapped.
func wrapLedgerError(err error) error {
	if err == nil {
		return nil
	}
	var e *errs.Error
	if errors.As(err, &e) {
		return e
	}
	return errs.New(errs.InvalidInput).Explain("That transaction isn't valid. Check the accounts, amounts, and dates you entered.").Wrap(err)
}

// wrapTransferError is wrapLedgerError's transfer-specific counterpart: it
// recognises the ledger sentinel errors ledger.NewTransfer can return and
// gives each one a specific, user-safe message and field, falling back to
// wrapLedgerError's generic message for anything else. By the time this
// package calls ledger.NewTransfer, the accounts are already known
// distinct-or-not and same-currency-or-not from data it fetched itself, so
// in practice only ErrTransferSameAccount and
// ErrCrossCurrencyTransferUnsupported are reachable here — but every
// sentinel is handled so this stays correct if that ever changes.
func wrapTransferError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ledger.ErrTransferSameAccount):
		return errs.New(errs.InvalidInput).
			Explain("A transfer's two accounts must be different.").
			Field("to_account_ref")
	case errors.Is(err, ledger.ErrCrossCurrencyTransferUnsupported):
		return errs.New(errs.InvalidInput).
			Explain("Transfers between accounts with different currencies aren't supported yet.").
			Field("to_account_ref").
			Wrap(err)
	case errors.Is(err, ledger.ErrTransferNotBalanced), errors.Is(err, ledger.ErrTransferPostingsMustOppose),
		errors.Is(err, ledger.ErrTransferPostingHasCategory), errors.Is(err, ledger.ErrTransferPostingCount):
		// These can't actually happen given how this package builds a
		// transfer's two postings (opposite signs, no category, exactly
		// two) — Internal, not InvalidInput, because reaching this branch
		// would mean this package's own invariant broke, not that the
		// user did anything wrong.
		return errs.New(errs.Internal).Explain("That transfer couldn't be recorded.").Wrap(err)
	default:
		return wrapLedgerError(err)
	}
}
