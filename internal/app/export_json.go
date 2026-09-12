package app

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// jsonEnvelope is the canonical JSON export's wire shape (ADR-0008:
// "bodger.export/v1... complete, versioned, deterministic... independent
// of the database schema"). Every field here is a fixed struct field, not
// a map, and every slice is already sorted by ExportSnapshot before it
// reaches this file -- together, that's what makes json.Marshal's output
// byte-identical across repeated calls on the same snapshot: Go's
// encoding/json always emits struct fields in declaration order, so the
// only remaining source of nondeterminism would be an unsorted slice or a
// map, and this file has neither.
//
// There is deliberately no "generated_at" or similar field: ADR-0008 is
// explicit that the payload carries "no timestamps of the export itself".
type jsonEnvelope struct {
	Format       string            `json:"format"`
	Accounts     []jsonAccount     `json:"accounts"`
	Categories   []jsonCategory    `json:"categories"`
	Transactions []jsonExportedTxn `json:"transactions"`
}

type jsonAccount struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Kind               string      `json:"kind"`
	OpeningBalance     money.Money `json:"opening_balance"`
	OpeningBalanceDate *string     `json:"opening_balance_date,omitempty"`
	Institution        *string     `json:"institution,omitempty"`
	SortOrder          int         `json:"sort_order"`
	ArchivedAt         *string     `json:"archived_at,omitempty"`
}

type jsonCategory struct {
	ID         string  `json:"id"`
	ParentID   *string `json:"parent_id,omitempty"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	SortOrder  int     `json:"sort_order"`
	ArchivedAt *string `json:"archived_at,omitempty"`
}

type jsonPosting struct {
	ID         string      `json:"id"`
	AccountID  string      `json:"account_id"`
	CategoryID *string     `json:"category_id,omitempty"`
	Amount     money.Money `json:"amount"`
	SortOrder  int         `json:"sort_order"`
}

type jsonExportedTxn struct {
	ID                   string        `json:"id"`
	Kind                 string        `json:"kind"`
	BookedDate           string        `json:"booked_date"`
	PostedDate           *string       `json:"posted_date,omitempty"`
	Description          string        `json:"description"`
	Notes                string        `json:"notes,omitempty"`
	ImportRecordID       *string       `json:"import_record_id,omitempty"`
	ExternalID           *string       `json:"external_id,omitempty"`
	RelatedTransactionID *string       `json:"related_transaction_id,omitempty"`
	Tags                 []string      `json:"tags,omitempty"`
	Postings             []jsonPosting `json:"postings"`
}

// toJSONEnvelope converts snapshot into jsonEnvelope's wire shape.
// snapshot's own slices are already sorted (ExportSnapshot's contract);
// this function preserves that order exactly rather than re-deriving it.
func toJSONEnvelope(snapshot ExportSnapshot) jsonEnvelope {
	accounts := make([]jsonAccount, 0, len(snapshot.Accounts))
	for _, a := range snapshot.Accounts {
		var obDate *string
		if d, ok := a.OpeningBalanceDate(); ok {
			s := d.String()
			obDate = &s
		}
		var institution *string
		if v, ok := a.Institution(); ok {
			institution = &v
		}
		var archivedAt *string
		if d, ok := a.ArchivedAt(); ok {
			s := d.String()
			archivedAt = &s
		}
		accounts = append(accounts, jsonAccount{
			ID:                 a.ID(),
			Name:               a.Name(),
			Kind:               string(a.Kind()),
			OpeningBalance:     a.OpeningBalance(),
			OpeningBalanceDate: obDate,
			Institution:        institution,
			SortOrder:          a.SortOrder(),
			ArchivedAt:         archivedAt,
		})
	}

	categories := make([]jsonCategory, 0, len(snapshot.Categories))
	for _, c := range snapshot.Categories {
		var parentID *string
		if id, ok := c.ParentID(); ok {
			parentID = &id
		}
		var archivedAt *string
		if d, ok := c.ArchivedAt(); ok {
			s := d.String()
			archivedAt = &s
		}
		categories = append(categories, jsonCategory{
			ID:         c.ID(),
			ParentID:   parentID,
			Name:       c.Name(),
			Kind:       string(c.Kind()),
			SortOrder:  c.SortOrder(),
			ArchivedAt: archivedAt,
		})
	}

	transactions := make([]jsonExportedTxn, 0, len(snapshot.Transactions))
	for _, et := range snapshot.Transactions {
		t := et.Transaction

		var postedDate *string
		if d, ok := t.PostedDate(); ok {
			s := d.String()
			postedDate = &s
		}
		var importRecordID *string
		if v, ok := t.ImportRecordID(); ok {
			importRecordID = &v
		}
		var externalID *string
		if v, ok := t.ExternalID(); ok {
			externalID = &v
		}
		var relatedTransactionID *string
		if v, ok := t.RelatedTransactionID(); ok {
			relatedTransactionID = &v
		}

		tags := make([]string, 0, len(et.Tags))
		for _, tag := range et.Tags {
			tags = append(tags, tag.String())
		}
		sortTagStrings(tags)

		postings := sortedPostings(t)
		jsonPostings := make([]jsonPosting, 0, len(postings))
		for _, p := range postings {
			var categoryID *string
			if id, ok := p.CategoryID(); ok {
				categoryID = &id
			}
			jsonPostings = append(jsonPostings, jsonPosting{
				ID:         p.ID(),
				AccountID:  p.AccountID(),
				CategoryID: categoryID,
				Amount:     p.Amount(),
				SortOrder:  p.SortOrder(),
			})
		}

		transactions = append(transactions, jsonExportedTxn{
			ID:                   t.ID(),
			Kind:                 string(t.Kind()),
			BookedDate:           t.BookedDate().String(),
			PostedDate:           postedDate,
			Description:          t.Description(),
			Notes:                t.Notes(),
			ImportRecordID:       importRecordID,
			ExternalID:           externalID,
			RelatedTransactionID: relatedTransactionID,
			Tags:                 tags,
			Postings:             jsonPostings,
		})
	}

	return jsonEnvelope{
		Format:       ExportFormatVersion,
		Accounts:     accounts,
		Categories:   categories,
		Transactions: transactions,
	}
}

// writeCanonicalJSON encodes snapshot as ADR-0008's canonical JSON
// document: 2-space indented, struct-field order (never a map), every
// slice already in ExportSnapshot's deterministic ID order. Calling this
// twice on two ExportSnapshot values built from identical underlying data
// produces byte-identical output -- that determinism is this function's
// whole job, and it's what the unit tests assert.
func writeCanonicalJSON(snapshot ExportSnapshot) ([]byte, error) {
	envelope := toJSONEnvelope(snapshot)
	buf, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, errs.New(errs.Internal).Wrap(err)
	}
	// A trailing newline makes the file POSIX-friendly (diffable, safe to
	// concatenate) without affecting json.Unmarshal, which ignores
	// trailing whitespace.
	return append(buf, '\n'), nil
}

// ExportJSONQuery is ExportJSON's query -- currently just ExportSnapshotQuery
// forwarded unchanged, kept as its own named type (rather than reusing
// ExportSnapshotQuery directly) so a future JSON-specific option (e.g. an
// explicit format-version override) doesn't change ExportSnapshot's own
// signature too.
type ExportJSONQuery struct {
	ActorID string
}

// ExportJSON implements issue #209's canonical-JSON export use case: fetch
// the actor's full domain snapshot, then encode it as ADR-0008's
// bodger.export/v1 document.
func (s *Service) ExportJSON(ctx context.Context, q ExportJSONQuery) ([]byte, error) {
	snapshot, err := s.ExportSnapshot(ctx, ExportSnapshotQuery(q))
	if err != nil {
		return nil, err
	}
	return writeCanonicalJSON(snapshot)
}

// sortTagStrings sorts tags in place, ascending. Tags on a transaction have
// no inherent order (data-model.md §6 treats them as an unordered set), so
// this is the one place export imposes a deterministic order on them,
// rather than preserving whatever order ports.TransactionRepository.Get
// happened to return (which is already tag_value order per that adapter's
// own query, but re-sorting here means this file's determinism doesn't
// depend on that adapter detail either).
func sortTagStrings(tags []string) {
	sort.Strings(tags)
}
