package ledger_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
)

func TestNewTag(t *testing.T) {
	t.Parallel()

	t.Run("normalises valid input", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name string
			raw  string
			want string
		}{
			{"already normalised", "groceries", "groceries"},
			{"leading hash is stripped", "#reimbursable", "reimbursable"},
			{"uppercase is lowercased", "TaxDeductible", "taxdeductible"},
			{"spaces become hyphens", "Japan Trip 2026", "japan-trip-2026"},
			{"hash and spaces together", "#Japan Trip 2026", "japan-trip-2026"},
			{"underscores become hyphens", "Tax_Deductible", "tax-deductible"},
			{"surrounding whitespace is trimmed", "  reimbursable  ", "reimbursable"},
			{"internal punctuation collapses to one hyphen", "a!!!b", "a-b"},
			{"non-leading hash is kebab-cased, not stripped", "a#b", "a-b"},
			{"trailing punctuation is dropped, not hyphenated", "reimbursable!!!", "reimbursable"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				tag, err := ledger.NewTag(tc.raw)
				if err != nil {
					t.Fatalf("NewTag(%q) = %v, want success", tc.raw, err)
				}
				if got := tag.String(); got != tc.want {
					t.Errorf("NewTag(%q).String() = %q, want %q", tc.raw, got, tc.want)
				}
			})
		}
	})

	t.Run("rejects input that normalises to nothing", func(t *testing.T) {
		t.Parallel()
		for _, raw := range []string{"", "   ", "#", "###", "!!!", "  #  "} {
			_, err := ledger.NewTag(raw)
			if !errors.Is(err, ledger.ErrInvalidTag) {
				t.Errorf("NewTag(%q) error = %v, want ErrInvalidTag", raw, err)
			}
		}
	})
}
