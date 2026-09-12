// Package importparse turns a source import file's raw bytes into
// normalised rows — ADR-0008's parser stage of the staged import pipeline:
// "format-aware and domain-ignorant: it does not resolve accounts, does not
// create transactions, does not know what a posting is." Every ambiguous
// field (dates, amounts, currency, free text) is resolved through
// internal/app/normalize, the same as every other surface, rather than
// this package growing a second, CSV-specific parser for any of them
// (ADR-0005's "an import parsing dates or amounts with its own logic would
// be a fifth surface disagreeing with the other four").
//
// CSVParser is the first (and, for now, only) Parser implementation, per
// issue #210's scope. Resolving an account, a category, or a duplicate
// candidate against the actor's own data is a later pipeline stage — this
// package's own output (Row) carries only what a parser can know from the
// file alone.
package importparse

import (
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

// Row is one source row, fully normalised — the parser's entire output.
// Nothing here has been resolved against the actor's own data: that's the
// mapping and duplicate-detection stages, one layer up in internal/app.
type Row struct {
	// RawPayload is the source row, reconstructed from its parsed fields —
	// ImportRecord's audit trail of what the parser saw. It is a faithful
	// re-serialisation of the row's field values, not necessarily
	// byte-identical to the original line: original quoting and spacing
	// are not preserved.
	RawPayload string
	// BookedDate is the row's reporting date (data-model.md §9), resolved
	// through normalize.DateOf.
	BookedDate domain.Date
	// PostedDate is the date the source itself reported, if the mapping
	// names a column for it and this row's cell isn't blank.
	PostedDate *domain.Date
	// Description is the row's normalised free-text description.
	Description string
	// Amount is the row's signed amount, in its own currency (from a
	// mapped currency column) or the parser's fallback currency.
	Amount money.Money
	// ExternalID is the source system's own ID for this row, trimmed but
	// otherwise untouched — opaque source data, not something
	// normalize.Text should reshape. Empty if the mapping has no such
	// column, or this row's cell is blank.
	ExternalID string
	// CategoryHint is the row's raw category text, if the mapping names a
	// column for it: a hint the mapping stage tries to resolve against the
	// actor's own categories. Trimmed but not otherwise normalised, for
	// the same reason as ExternalID.
	CategoryHint string
	// SortOrder is the row's zero-based position among the file's data
	// rows (the header, if any, doesn't count) — ImportRecord.SortOrder's
	// source value.
	SortOrder int
}

// Parser turns a source file's raw bytes into normalised Rows. Every
// implementation is format-specific — ADR-0008: "adding a new format is a
// new parser and nothing else" — which is what this narrow interface is
// for: nothing about column mapping, resolving accounts/categories, or
// detecting duplicates belongs here.
type Parser interface {
	Parse(data []byte) ([]Row, error)
}
