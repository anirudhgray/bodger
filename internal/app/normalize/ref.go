package normalize

import (
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// Candidate is one named, identified thing Ref can resolve raw input
// against — typically every account or category the actor owns, fetched
// by the use-case calling Ref (issue #6) via
// ports.AccountRepository.List or ports.CategoryRepository.List. Ref
// itself does no repository I/O, like every other function in this
// package: it's a pure resolution over a candidate set someone else
// fetched.
type Candidate struct {
	ID   string
	Name string
}

// Ref resolves raw — an AccountRef or CategoryRef command field
// (ADR-0005) — against candidates, in this order:
//
//  1. raw parses as a UUID: if some candidate has that ID, it wins.
//     (Parsing as a UUID is not itself enough — only the actor's own
//     candidate set, already scoped by the caller, is authoritative.)
//  2. An exact, case-sensitive name match.
//  3. A unique case-insensitive name match.
//  4. Otherwise, a not-found error if nothing matched at all, or a
//     disambiguation error listing every candidate whose name matched
//     case-insensitively.
func Ref(raw string, candidates []Candidate) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errs.New(errs.InvalidInput).Explain("reference must not be empty")
	}

	if _, err := uuid.Parse(trimmed); err == nil {
		for _, c := range candidates {
			if c.ID == trimmed {
				return c.ID, nil
			}
		}
		return "", errs.New(errs.NotFound).Explain("no match for %q", raw)
	}

	for _, c := range candidates {
		if c.Name == trimmed {
			return c.ID, nil
		}
	}

	var matches []Candidate
	for _, c := range candidates {
		if strings.EqualFold(c.Name, trimmed) {
			matches = append(matches, c)
		}
	}

	switch len(matches) {
	case 0:
		return "", errs.New(errs.NotFound).Explain("no match for %q", raw)
	case 1:
		return matches[0].ID, nil
	default:
		names := make([]string, len(matches))
		for i, c := range matches {
			names[i] = c.Name
		}
		sort.Strings(names)
		return "", errs.New(errs.InvalidInput).
			Explain("%q matches more than one: %s", raw, strings.Join(names, ", ")).
			With("candidates", names)
	}
}
