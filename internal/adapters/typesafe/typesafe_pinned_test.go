package typesafe_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/anirudhgray/bodger/internal/adapters/typesafe"
	"github.com/anirudhgray/bodger/internal/ports"
)

// These two tests are the point of this package (issue #304): they pin
// the exact wire shape a request may take, so a future "simplification" of
// the request builder — e.g. replacing the explicit stateBody/questionBody
// projection with json.Marshal(row) on the whole ports.SuggestionRow, or
// batching several rows into one call — fails loudly here rather than
// quietly reintroducing ADR-0015's privacy leak or accuracy regression.

// TestPinned_RequestBodyNeverCarriesMoreThanSuggestionRow asserts the
// serialised request body for a row never contains RawPayload (the field
// that makes importing.ImportRecord unsafe to send verbatim), the row's
// internal RecordID (which has no reason to leave the machine — it's used
// only to correlate the response locally), or any top-level/state key
// beyond the fixed wire shape this adapter is supposed to send.
//
// If someone "simplifies" buildRequestBody into marshalling the whole
// ports.SuggestionRow (or, worse, an importing.ImportRecord) directly,
// this test fails: RecordID appears in the body, and/or extra keys
// (categories, occurrenceCandidates, recordId, rawPayload, ...) show up
// where only description/amount/currency/date are allowed.
func TestPinned_RequestBodyNeverCarriesMoreThanSuggestionRow(t *testing.T) {
	const sentinelRecordID = "internal-db-record-id-must-never-be-sent-to-the-vendor"

	var capturedBody []byte
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		capturedBody = body
		return jsonResponse(200, `{"answers": {"category": {"choice": "cat-food", "confidence": 0.8}}}`), nil
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	row := categoryRow(sentinelRecordID)
	row.OccurrenceCandidates = []ports.OccurrenceOption{{OccurrenceID: "occ-1", Description: "Monthly rent"}}

	if _, _, err := p.Suggest(context.Background(), []ports.SuggestionRow{row}); err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if capturedBody == nil {
		t.Fatal("no request body was captured")
	}

	bodyText := string(capturedBody)
	if strings.Contains(bodyText, "RawPayload") || strings.Contains(bodyText, "rawPayload") || strings.Contains(bodyText, "raw_payload") {
		t.Fatalf("request body contains RawPayload, which must never leave the machine (ADR-0015): %s", bodyText)
	}
	if strings.Contains(bodyText, sentinelRecordID) {
		t.Fatalf("request body contains the row's RecordID, which the vendor has no reason to see: %s", bodyText)
	}

	var decoded map[string]any
	if err := json.Unmarshal(capturedBody, &decoded); err != nil {
		t.Fatalf("request body isn't valid JSON: %v", err)
	}

	wantTopLevel := map[string]bool{"model": true, "state": true, "questions": true}
	for key := range decoded {
		if !wantTopLevel[key] {
			t.Errorf("request body has unexpected top-level key %q — only model/state/questions may appear", key)
		}
	}

	state, ok := decoded["state"].(map[string]any)
	if !ok {
		t.Fatalf("state is %T, want a JSON object (never an array of rows)", decoded["state"])
	}
	wantStateKeys := map[string]bool{"description": true, "amount": true, "currency": true, "date": true}
	for key := range state {
		if !wantStateKeys[key] {
			t.Errorf("state has unexpected key %q — only description/amount/currency/date may appear (ADR-0015 'What leaves the machine')", key)
		}
	}
	for key := range wantStateKeys {
		if _, ok := state[key]; !ok {
			t.Errorf("state is missing expected key %q", key)
		}
	}
}

// TestPinned_NeverMoreThanOneRowPerRequest issues Suggest over three rows
// with distinguishable descriptions and asserts: (1) exactly three HTTP
// requests are made — one per row — and (2) every single request's body
// contains exactly one of the three descriptions, never two or three.
//
// If someone "simplifies" the fan-out into one request carrying every
// row's state (the batching ADR-0015 explicitly rejects on accuracy
// grounds — "every question mostly distractor content"), this test fails:
// either the call count drops to 1, or a request body contains more than
// one row's description.
func TestPinned_NeverMoreThanOneRowPerRequest(t *testing.T) {
	descriptions := []string{"MERCHANT-ALPHA-desc", "MERCHANT-BRAVO-desc", "MERCHANT-CHARLIE-desc"}

	var mu sync.Mutex
	var capturedBodies [][]byte
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		mu.Lock()
		capturedBodies = append(capturedBodies, body)
		mu.Unlock()
		return jsonResponse(200, `{"answers": {"category": {"choice": "cat-food", "confidence": 0.8}}}`), nil
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	rows := make([]ports.SuggestionRow, len(descriptions))
	for i, desc := range descriptions {
		row := categoryRow(descriptions[i])
		row.Description = desc
		rows[i] = row
	}

	if _, _, err := p.Suggest(context.Background(), rows); err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	if len(capturedBodies) != len(rows) {
		t.Fatalf("got %d HTTP requests for %d rows, want exactly one request per row", len(capturedBodies), len(rows))
	}

	for i, body := range capturedBodies {
		bodyText := string(body)
		seen := 0
		for _, desc := range descriptions {
			if strings.Contains(bodyText, desc) {
				seen++
			}
		}
		if seen != 1 {
			t.Errorf("request %d contains %d of the %d rows' descriptions (%s), want exactly 1 — no request may carry more than one row",
				i, seen, len(descriptions), bodyText)
		}
	}
}
