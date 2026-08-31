package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
)

// timeLayout is the TEXT representation this package stores timestamps in:
// RFC 3339 with nanosecond precision, always UTC. SQLite has no native
// datetime type; a fixed, sortable string format is what makes ORDER BY and
// range comparisons on these columns behave.
const timeLayout = time.RFC3339Nano

// formatTime renders t for storage. Every timestamp this package writes is
// normalised to UTC first, so a column's sort order matches its
// chronological order regardless of what zone the value started in.
func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// dateLayout is the TEXT representation this package stores domain.Date
// values in: plain ISO 8601 calendar dates, matching domain.Date.String().
const dateLayout = "2006-01-02"

// formatDate renders d for storage.
func formatDate(d domain.Date) string {
	return d.String()
}

// parseDate parses a TEXT column written by formatDate.
func parseDate(s string) (domain.Date, error) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return domain.Date{}, fmt.Errorf("sqlite: parse stored date %q: %w", s, err)
	}
	return domain.NewDate(t.Year(), t.Month(), t.Day())
}

// nullableDate converts an optional domain.Date (as returned by the
// "(domain.Date, bool)" accessor pattern used across internal/domain/ledger)
// into a sql.NullString for a nullable TEXT column.
func nullableDate(d domain.Date, ok bool) sql.NullString {
	if !ok {
		return sql.NullString{}
	}
	return sql.NullString{String: formatDate(d), Valid: true}
}

// nullableString converts an optional string (the "(string, bool)" pattern
// used for optional IDs across internal/domain/ledger) into a
// sql.NullString for a nullable TEXT column.
func nullableString(s string, ok bool) sql.NullString {
	if !ok {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
