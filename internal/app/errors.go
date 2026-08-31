package app

import (
	"errors"

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
