package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"strings"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// csvHeader is the CSV export's fixed column order (ADR-0008: "one row per
// posting with the transaction denormalised across it"). A column is never
// reordered or removed once shipped -- that would silently break any
// spreadsheet or script a user built against a previous export -- so a
// future column is always appended at the end, never inserted.
var csvHeader = []string{
	"transaction_id",
	"booked_date",
	"posted_date",
	"kind",
	"description",
	"notes",
	"tags",
	"external_id",
	"related_transaction_id",
	"posting_id",
	"account_id",
	"category_id",
	"amount",
	"currency",
}

// csvTagSeparator joins a transaction's tags into the CSV export's single
// "tags" cell. "|" rather than "," (which would collide with a second tag
// containing a comma before CSV quoting even enters the picture, since this
// join happens before the row is handed to encoding/csv) or ";" (which
// some spreadsheet locales treat as a field separator on paste) -- a tag
// is already restricted to letters, digits, and hyphens by
// ledger.NewTag/normalize.Tag, so "|" can never collide with real tag
// content.
const csvTagSeparator = "|"

// writeCSVRows builds the CSV export's rows (header first, one row per
// posting after it) from snapshot.Transactions, in the same order
// ExportSnapshot already sorted them: transaction ID, then each
// transaction's own postings via sortedPostings. ADR-0008 calls this
// format "lossy by design": a transaction's own account-independent facts
// (dates, description, notes, tags) are repeated on every one of its
// posting rows, and there is nothing here that lets a reader reconstruct
// which postings belonged to the same split without grouping by
// transaction_id itself -- CSV is for humans and spreadsheets, not
// round-tripping (ADR-0008's own words: "explicitly not an interchange
// format").
//
// Accounts and categories are not rows in this file at all: a posting
// names its account_id and category_id, but resolving those to a name
// means joining against the canonical JSON export (or an API call) --
// exactly the denormalisation CSV's own definition calls for.
func writeCSVRows(snapshot ExportSnapshot) [][]string {
	rows := make([][]string, 0, 1+len(snapshot.Transactions))
	rows = append(rows, csvHeader)

	for _, et := range snapshot.Transactions {
		t := et.Transaction

		postedDate := ""
		if d, ok := t.PostedDate(); ok {
			postedDate = d.String()
		}
		externalID := ""
		if v, ok := t.ExternalID(); ok {
			externalID = v
		}
		relatedTransactionID := ""
		if v, ok := t.RelatedTransactionID(); ok {
			relatedTransactionID = v
		}

		tags := make([]string, 0, len(et.Tags))
		for _, tag := range et.Tags {
			tags = append(tags, tag.String())
		}
		sortTagStrings(tags)
		tagsCell := strings.Join(tags, csvTagSeparator)

		for _, p := range sortedPostings(t) {
			categoryID := ""
			if id, ok := p.CategoryID(); ok {
				categoryID = id
			}
			rows = append(rows, []string{
				t.ID(),
				t.BookedDate().String(),
				postedDate,
				string(t.Kind()),
				t.Description(),
				t.Notes(),
				tagsCell,
				externalID,
				relatedTransactionID,
				p.ID(),
				p.AccountID(),
				categoryID,
				p.Amount().AmountString(),
				p.Amount().Currency(),
			})
		}
	}

	return rows
}

// writeCSV encodes snapshot as ADR-0008's CSV export using Go's standard
// encoding/csv writer (RFC 4180 quoting), so quoting a description or note
// containing a comma, quote, or newline is exactly as reliable as
// encoding/csv's own well-tested behaviour rather than a hand-rolled
// escaper. Calling this twice on two ExportSnapshot values built from
// identical underlying data produces byte-identical output, the same
// determinism guarantee writeCanonicalJSON gives the JSON export.
func writeCSV(snapshot ExportSnapshot) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.WriteAll(writeCSVRows(snapshot)); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	return buf.Bytes(), nil
}

// ExportCSVQuery is ExportCSV's query -- see ExportJSONQuery's doc comment
// for why this isn't just ExportSnapshotQuery reused directly.
type ExportCSVQuery struct {
	ActorID string
}

// ExportCSV implements issue #209's CSV export use case: fetch the actor's
// full domain snapshot, then encode it as ADR-0008's one-row-per-posting
// CSV.
func (s *Service) ExportCSV(ctx context.Context, q ExportCSVQuery) ([]byte, error) {
	snapshot, err := s.ExportSnapshot(ctx, ExportSnapshotQuery(q))
	if err != nil {
		return nil, err
	}
	return writeCSV(snapshot)
}
