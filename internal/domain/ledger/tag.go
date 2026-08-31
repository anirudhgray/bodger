package ledger

import (
	"fmt"
	"strings"
	"unicode"
)

// Tag is a free-form label on a Transaction — never on a Posting, because
// a tag describes the event, not the split line (data-model.md §6). Its
// field is unexported: the only way to produce one is NewTag, which
// normalises raw input the same way every time it's called, so the same
// string always produces the same Tag regardless of which surface it came
// from.
//
// NewTag is what internal/app/normalize.Tag calls (issue #5, not built
// here). Surfaces themselves pass tags through as raw strings, like every
// other command field — ADR-0005.
type Tag struct {
	value string
}

// NewTag normalises raw into a Tag: lowercases it, strips a single leading
// "#", and kebab-cases whatever's left — runs of anything that isn't a
// letter or digit collapse to a single hyphen, and leading/trailing
// hyphens are dropped entirely. It returns ErrInvalidTag if nothing is
// left once that's done.
func NewTag(raw string) (Tag, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "#")
	s = strings.ToLower(s)

	var b strings.Builder
	pendingHyphen := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingHyphen {
				b.WriteByte('-')
				pendingHyphen = false
			}
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			pendingHyphen = true
		}
	}

	value := b.String()
	if value == "" {
		return Tag{}, fmt.Errorf("%w: %q", ErrInvalidTag, raw)
	}
	return Tag{value: value}, nil
}

// String returns the normalised tag value.
func (t Tag) String() string { return t.value }
