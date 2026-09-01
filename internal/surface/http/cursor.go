package http

import (
	"encoding/base64"
	"encoding/json"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// cursorPayload is the opaque cursor's actual content: an offset into
// app.ListTransactionsQuery's offset-paginated result set. It's private
// to this file and never documented as part of the API's contract — a
// client is only ever told "pass the cursor value you were given back as
// the next page's cursor query parameter", never what's inside it, so
// this encoding can change without being a breaking change.
type cursorPayload struct {
	Offset int `json:"o"`
}

// encodeCursor renders offset as an opaque cursor token, or "" for
// offset 0 (the first page never needs one).
func encodeCursor(offset int) string {
	if offset <= 0 {
		return ""
	}
	b, err := json.Marshal(cursorPayload{Offset: offset})
	if err != nil {
		// cursorPayload is a struct with a single int field — this
		// cannot fail in practice.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeCursor reverses encodeCursor. An empty raw cursor decodes to
// offset 0 (the first page). Anything that doesn't decode to a
// non-negative offset is rejected as InvalidInput rather than silently
// treated as page one — a client whose cursor was corrupted or hand-edited
// should find out, not quietly restart its pagination.
func decodeCursor(raw string) (int, *errs.Error) {
	if raw == "" {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, invalidCursorErr(raw)
	}
	var p cursorPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return 0, invalidCursorErr(raw)
	}
	if p.Offset < 0 {
		return 0, invalidCursorErr(raw)
	}
	return p.Offset, nil
}

func invalidCursorErr(raw string) *errs.Error {
	return errs.New(errs.InvalidInput).Explain("%q isn't a valid page cursor.", raw).Field("cursor")
}
