//go:build typesafe_live

// This file only compiles under `go test -tags typesafe_live` (wired to
// `make check-typesafe-live`, see the Makefile and docs/contributing.md).
// It is excluded from `go build ./...`, `go test ./...`, `make check`, and
// CI entirely — not merely skipped at runtime — because it spends real
// typesafe.ai credits against a real account. A `t.Skip` on an unset
// environment variable alone is not enough: that would still compile this
// file into the default `go test ./...` run and (per issue #304) "a test
// that costs money and needs a secret must be impossible to run by
// accident." The build tag is what makes it impossible, not the skip
// below, which only handles the case of someone deliberately running the
// tagged target without having exported a key yet.

package typesafe_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/adapters/typesafe"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/config"
	"github.com/anirudhgray/bodger/internal/ports"
)

// TestLive_Suggest_RealAPI is deliberately minimal: one row, two
// questions, a handful of options, a single call — enough to catch the
// request or response schema drifting out from under this adapter, which
// no fake transport can detect (ADR-0015 "Testing: mocks in CI, credits
// never"). It is not a correctness test of the model's answers: it never
// asserts what category or occurrence gets chosen, only that a
// well-formed request gets a well-formed, decodable response back with no
// error.
func TestLive_Suggest_RealAPI(t *testing.T) {
	apiKey := os.Getenv(config.EnvTypesafeAPIKey)
	if apiKey == "" {
		t.Skipf("%s is not set — skipping the live typesafe.ai test (see docs/contributing.md)", config.EnvTypesafeAPIKey)
	}
	baseURL := os.Getenv(config.EnvTypesafeBaseURL)

	client := typesafe.New(apiKey, baseURL, nil)

	amount, err := money.NewMoney(-45000, "INR")
	if err != nil {
		t.Fatalf("money.NewMoney: %v", err)
	}
	date, err := domain.NewDate(2026, time.September, 3)
	if err != nil {
		t.Fatalf("domain.NewDate: %v", err)
	}

	row := ports.SuggestionRow{
		RecordID:    "live-test-row-1",
		Description: "SWIGGY*BANGALORE ORDER 8213",
		Amount:      amount,
		Date:        date,
		Categories: []ports.CategoryOption{
			{ID: "cat-food", Name: "Food & Dining"},
			{ID: "cat-transport", Name: "Transport"},
			{ID: "cat-shopping", Name: "Shopping"},
		},
		OccurrenceCandidates: []ports.OccurrenceOption{
			{OccurrenceID: "occ-rent", Description: "Monthly rent"},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got, err := client.Suggest(ctx, []ports.SuggestionRow{row})
	if err != nil {
		t.Fatalf("Suggest against the live API: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d suggestions, want 1 (schema drift?)", len(got))
	}
	if got[0].RecordID != row.RecordID {
		t.Errorf("RecordID = %q, want %q", got[0].RecordID, row.RecordID)
	}
	// CategoryConfidence and OccurrenceConfidence must be in [0, 1] if an
	// answer was given at all — a value outside that range would mean the
	// response schema changed underneath this adapter's assumptions.
	if got[0].CategoryID != "" && (got[0].CategoryConfidence < 0 || got[0].CategoryConfidence > 1) {
		t.Errorf("CategoryConfidence = %v, want a value in [0, 1]", got[0].CategoryConfidence)
	}
	if got[0].OccurrenceID != "" && (got[0].OccurrenceConfidence < 0 || got[0].OccurrenceConfidence > 1) {
		t.Errorf("OccurrenceConfidence = %v, want a value in [0, 1]", got[0].OccurrenceConfidence)
	}
}
