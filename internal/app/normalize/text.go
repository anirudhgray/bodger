package normalize

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// Text normalises a free-form text field — a transaction description, a
// note — the same way regardless of which surface it came from: Unicode
// NFC (so a precomposed "é" and "e" followed by a combining acute compare
// and store identically, whichever the input device happened to produce),
// trimmed, with runs of internal whitespace collapsed to a single space,
// and capped at maxLen runes. maxLen <= 0 means no length cap.
//
// Text doesn't set a field path on the error it returns — unlike Amount,
// Currency, and DateOf, it's called for more than one command field
// (Description, Notes, and future ones), so it can't know which one it's
// normalising. The caller attaches that: err's underlying *errs.Error can
// still be reached with errors.As and given a Field(...) before it's
// returned further, since Field mutates and returns the same pointer.
func Text(raw string, maxLen int) (string, error) {
	s := norm.NFC.String(raw)
	s = strings.TrimSpace(s)
	s = collapseWhitespace(s)

	if maxLen > 0 && utf8.RuneCountInString(s) > maxLen {
		return "", errs.New(errs.InvalidInput).Explain("must be %d characters or fewer", maxLen)
	}
	return s, nil
}

// collapseWhitespace replaces every run of one or more Unicode whitespace
// characters with a single ASCII space.
func collapseWhitespace(s string) string {
	var b strings.Builder
	pendingSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if b.Len() > 0 {
				pendingSpace = true
			}
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
