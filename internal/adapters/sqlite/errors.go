package sqlite

import (
	"errors"

	"modernc.org/sqlite"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// SQLite extended result codes this package branches on. These are stable,
// documented values from SQLite's own C API (https://www.sqlite.org/rescode.html),
// not modernc.org/sqlite internals, so hardcoding them avoids a dependency
// on that driver's internal "lib" subpackage for four numbers that don't
// change.
const (
	sqliteConstraintCheck      = 275  // SQLITE_CONSTRAINT_CHECK
	sqliteConstraintForeignKey = 787  // SQLITE_CONSTRAINT_FOREIGNKEY
	sqliteConstraintPrimaryKey = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
	sqliteConstraintUnique     = 2067 // SQLITE_CONSTRAINT_UNIQUE
)

// wrapWriteError classifies err from an INSERT or UPDATE into the
// *errs.Error a repository method should return (ADR-0011): a uniqueness
// or primary-key violation is a Conflict, a foreign-key or check violation
// is InvalidInput (the caller referenced something that doesn't exist, or
// gave a value the schema rejects), and anything else is Internal with the
// cause wrapped for the log. Every call site chains its own .Explain(...)
// onto the result for the end-user-safe message, so this only decides the
// code.
func wrapWriteError(err error) *errs.Error {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqliteConstraintUnique, sqliteConstraintPrimaryKey:
			return errs.New(errs.Conflict).Wrap(err)
		case sqliteConstraintForeignKey, sqliteConstraintCheck:
			return errs.New(errs.InvalidInput).Wrap(err)
		}
	}
	return errs.New(errs.Internal).Wrap(err)
}
