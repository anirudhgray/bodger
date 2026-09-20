package typesafe_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/adapters/typesafe"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// roundTripFunc lets a test fabricate an *http.Response (or a transport
// error) for any request without a real network call — the same pattern
// internal/adapters/fxprovider/frankfurter_test.go uses.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func clientReturning(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

// synchronizedRoundTripper wraps a roundTripFunc with a mutex, for tests
// that record state across concurrent calls from Suggest's worker pool.
func synchronizedRoundTripper(fn roundTripFunc) *http.Client {
	var mu sync.Mutex
	return clientReturning(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		return fn(r)
	})
}

func wantErrCode(t *testing.T, err error, want errs.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want code %q", want)
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("got error of type %T (%v), want *errs.Error with code %q", err, err, want)
	}
	if e.Code != want {
		t.Fatalf("got code %q, want %q (message: %s)", e.Code, want, e.Message)
	}
}

func categoryRow(recordID string) ports.SuggestionRow {
	return ports.SuggestionRow{
		RecordID:    recordID,
		Description: "SWIGGY*BANGALORE",
		Amount:      mustMoney(-45000, "INR"),
		Date:        mustDate(2026, time.September, 3),
		Categories: []ports.CategoryOption{
			{ID: "cat-food", Name: "Food & Dining"},
			{ID: "cat-transport", Name: "Transport"},
		},
	}
}

func rowWithOccurrence(recordID string) ports.SuggestionRow {
	row := categoryRow(recordID)
	row.OccurrenceCandidates = []ports.OccurrenceOption{
		{OccurrenceID: "occ-1", Description: "Monthly rent"},
	}
	return row
}

// --- normal answers, both question types ---------------------------------

func TestSuggest_NormalAnswer_BothQuestionTypes(t *testing.T) {
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{
			"answers": {
				"category": {"choice": "cat-food", "probabilities": {"cat-food": 0.9}, "confidence": 0.87},
				"occurrence": {"choice": "occ-1", "probabilities": {"occ-1": 0.7}, "confidence": 0.62}
			},
			"usage": {"input_tokens": 100, "output_tokens": 10}
		}`), nil
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	got, err := p.Suggest(context.Background(), []ports.SuggestionRow{rowWithOccurrence("rec-1")})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d suggestions, want 1", len(got))
	}
	s := got[0]
	if s.RecordID != "rec-1" {
		t.Errorf("RecordID = %q, want rec-1", s.RecordID)
	}
	if s.CategoryID != "cat-food" {
		t.Errorf("CategoryID = %q, want cat-food", s.CategoryID)
	}
	if s.CategoryConfidence != 0.87 {
		t.Errorf("CategoryConfidence = %v, want 0.87 (unfiltered pass-through)", s.CategoryConfidence)
	}
	if s.OccurrenceID != "occ-1" {
		t.Errorf("OccurrenceID = %q, want occ-1", s.OccurrenceID)
	}
	if s.OccurrenceConfidence != 0.62 {
		t.Errorf("OccurrenceConfidence = %v, want 0.62 (unfiltered pass-through)", s.OccurrenceConfidence)
	}
}

func TestSuggest_LowConfidenceStillPassedThrough(t *testing.T) {
	// ADR-0015: the 0.5 display threshold is app-layer policy. This
	// adapter must never drop or clamp a low confidence value itself.
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"answers": {"category": {"choice": "cat-food", "confidence": 0.02}}}`), nil
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	got, err := p.Suggest(context.Background(), []ports.SuggestionRow{categoryRow("rec-1")})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(got) != 1 || got[0].CategoryConfidence != 0.02 {
		t.Fatalf("got %+v, want a single suggestion with confidence 0.02 untouched", got)
	}
}

// --- "none of these" -------------------------------------------------------

func TestSuggest_NoneOfTheseIsEmptyIDNotError(t *testing.T) {
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{
			"answers": {
				"category": {"choice": "none_of_these", "confidence": 0.55},
				"occurrence": {"choice": "none_of_these", "confidence": 0.4}
			}
		}`), nil
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	got, err := p.Suggest(context.Background(), []ports.SuggestionRow{rowWithOccurrence("rec-1")})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d suggestions, want 1", len(got))
	}
	if got[0].CategoryID != "" {
		t.Errorf("CategoryID = %q, want empty (none of these)", got[0].CategoryID)
	}
	if got[0].OccurrenceID != "" {
		t.Errorf("OccurrenceID = %q, want empty (none of these)", got[0].OccurrenceID)
	}
}

func TestSuggest_EmptyOccurrenceCandidates_NoOccurrenceQuestionAsked(t *testing.T) {
	var sawOccurrenceQuestion bool
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		if strings.Contains(string(body), `"occurrence"`) {
			sawOccurrenceQuestion = true
		}
		return jsonResponse(200, `{"answers": {"category": {"choice": "cat-food", "confidence": 0.8}}}`), nil
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	row := categoryRow("rec-1") // no OccurrenceCandidates
	got, err := p.Suggest(context.Background(), []ports.SuggestionRow{row})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d suggestions, want 1", len(got))
	}
	if sawOccurrenceQuestion {
		t.Errorf("request carried an occurrence question for a row with no OccurrenceCandidates")
	}
}

// --- fan-out and partial failure -------------------------------------------

func TestSuggest_PartialFailure_ThreeRowsOneFails_TwoSuggestionsNoError(t *testing.T) {
	client := synchronizedRoundTripper(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		if strings.Contains(string(body), "SWIGGY-FAIL") {
			// 422 (not 500/429) so this test isn't slowed by the retry
			// policy's backoff — retry behaviour has its own tests.
			return jsonResponse(422, `{"message": "malformed question"}`), nil
		}
		return jsonResponse(200, `{"answers": {"category": {"choice": "cat-food", "confidence": 0.8}}}`), nil
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	rows := []ports.SuggestionRow{
		categoryRow("rec-1"),
		func() ports.SuggestionRow {
			r := categoryRow("rec-2")
			r.Description = "SWIGGY-FAIL"
			return r
		}(),
		categoryRow("rec-3"),
	}

	got, err := p.Suggest(context.Background(), rows)
	if err != nil {
		t.Fatalf("Suggest: %v (partial failure must not be an error)", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d suggestions, want 2 (one row failed, two succeeded)", len(got))
	}
}

func TestSuggest_EveryRowFails_ReturnsError(t *testing.T) {
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(401, `{"message": "invalid key"}`), nil
	})
	p := typesafe.New("bad-key", "http://example.invalid", client)

	got, err := p.Suggest(context.Background(), []ports.SuggestionRow{
		categoryRow("rec-1"),
		categoryRow("rec-2"),
	})
	if err == nil {
		t.Fatalf("Suggest: got nil error, want one (every row failed)")
	}
	if len(got) != 0 {
		t.Errorf("got %d suggestions, want 0", len(got))
	}
	wantErrCode(t, err, errs.Unavailable)
}

func TestSuggest_EmptyRows_ReturnsNilNil(t *testing.T) {
	p := typesafe.New("test-key", "http://example.invalid", clientReturning(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("no request should be sent for an empty row slice")
		return nil, nil
	}))

	got, err := p.Suggest(context.Background(), nil)
	if err != nil || got != nil {
		t.Fatalf("Suggest(nil) = %v, %v, want nil, nil", got, err)
	}
}

func TestSuggest_RowWithNoQuestions_SkippedSilently(t *testing.T) {
	p := typesafe.New("test-key", "http://example.invalid", clientReturning(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("no request should be sent for a row with no categories and no occurrence candidates")
		return nil, nil
	}))

	row := ports.SuggestionRow{RecordID: "rec-1", Description: "mystery", Amount: mustMoney(-100, "INR"), Date: mustDate(2026, time.January, 1)}
	got, err := p.Suggest(context.Background(), []ports.SuggestionRow{row})
	if err != nil {
		t.Fatalf("Suggest: %v, want nil (nothing to ask isn't a failure)", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d suggestions, want 0", len(got))
	}
}
