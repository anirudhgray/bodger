// Package typesafe implements ports.SuggestionProvider against typesafe.ai's
// System One HTTP API (docs/decisions/0015-ai-assisted-suggestions.md).
// There is no official Go SDK (Python and JavaScript only), so this is a
// hand-written HTTP client covering exactly the one endpoint and the one
// question type ("choice") this feature uses — the same size and shape as
// internal/adapters/fxprovider, on that package's precedent.
//
// The one rule every file in this package answers to is ADR-0015's "What
// leaves the machine": the adapter only ever sees a ports.SuggestionRow,
// never an importing.ImportRecord, and only ever puts that row's
// description, amount, currency, and date on the wire — never its
// RecordID, never a second row's content, and never the request or its
// credentials into a returned error.
package typesafe

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anirudhgray/bodger/internal/ports"
)

// Model is the pinned typesafe.ai model version this adapter targets.
// ADR-0015 is explicit: never "jev-latest" — confidence is calibrated per
// model version, and an alias would move that calibration out from under
// bodger without a release on this side. Bumping it is a deliberate,
// separate PR that starts by re-reading that version's published
// jaggedness page, since the date/amount workarounds this feature relies
// on are documented per version.
const Model = "jev-1.13.0"

// DefaultBaseURL is typesafe.ai's hosted API. An operator pointing at a
// different instance passes a different baseURL to New (ADR-0015's
// BODGER_TYPESAFE_BASE_URL, resolved by internal/platform/config — this
// package has no opinion on where the value came from).
const DefaultBaseURL = "https://api.typesafe.ai"

// DefaultTimeout bounds a single per-row HTTP request. It is not the
// overall Suggest deadline — that's the caller's context — but a floor
// under it, so one hung TCP connection inside a bounded worker pool cannot
// occupy a worker slot indefinitely.
const DefaultTimeout = 30 * time.Second

// workerPoolWidth is how many per-row requests Suggest issues concurrently.
// ADR-0015 requires this to be a package constant, not configuration: "an
// operator who wants a different number is better served by the
// implementation issue revisiting it with real usage than by a knob nobody
// knows to turn" (the same reasoning the ADR applies to the 200-row cap).
const workerPoolWidth = 5

// noneOfTheseOption is the criteria key reserved on every Choice question
// for "none of these apply" (ADR-0015: "every Choice includes an explicit
// 'none of these' option so the model has somewhere honest to put an
// unmatched row"). Real option IDs come from bodger's own generated
// identifiers (see internal/platform/idgen), which never collide with this
// literal.
const noneOfTheseOption = "none_of_these"

// categoryQuestionID and occurrenceQuestionID are this adapter's fixed
// question keys. They are typesafe.ai request/response vocabulary, never
// exposed above the port.
const (
	categoryQuestionID   = "category"
	occurrenceQuestionID = "occurrence"
)

// Client is a ports.SuggestionProvider backed by typesafe.ai's System One
// API. It holds no mutable state beyond its configuration and is safe for
// concurrent use — Suggest itself fans out concurrently through a bounded
// worker pool.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

var _ ports.SuggestionProvider = (*Client)(nil)

// New constructs a typesafe.ai adapter. apiKey is the instance's
// credential (ADR-0015's BODGER_TYPESAFE_API_KEY — resolving whether it's
// even set, and therefore whether this adapter should be constructed at
// all, is the application layer's job per ADR-0015's "Configured: false"
// result field, not this package's). baseURL is normally empty, which
// resolves to DefaultBaseURL; any other value points at a self-hosted or
// proxied instance. client is normally nil, which constructs an
// *http.Client with DefaultTimeout; a caller with its own transport
// requirements — including a test pointing at a fake transport — can pass
// any *http.Client.
func New(apiKey, baseURL string, client *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: client,
	}
}

// rowResult is one worker's outcome for one row. attempted is false when
// the row had nothing worth asking about (no categories and no occurrence
// candidates) — that is not a failure, per ADR-0015's "the model is never
// invited to invent a match out of an empty or irrelevant set", and it
// must never count towards "every row failed".
type rowResult struct {
	suggestion ports.RowSuggestion
	attempted  bool
	err        error
}

// Suggest implements ports.SuggestionProvider. It issues one HTTP request
// per row (ADR-0015 "One request per row"), concurrently through a
// workerPoolWidth-wide worker pool, and honours ctx for both cancellation
// and the overall deadline — no goroutine outlives this call.
//
// Partial success is success: the returned slice carries every suggestion
// obtained, and an error is returned only when every attempted row failed.
// A row skipped for having nothing to ask is neither a success nor a
// failure — it simply has no entry, per the port's "sparse and unordered"
// contract.
func (c *Client) Suggest(ctx context.Context, rows []ports.SuggestionRow) ([]ports.RowSuggestion, error) {
	if len(rows) == 0 {
		return nil, nil
	}

	rowCh := make(chan ports.SuggestionRow)
	resultCh := make(chan rowResult, len(rows))

	var wg sync.WaitGroup
	for i := 0; i < workerPoolWidth; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for row := range rowCh {
				suggestion, attempted, err := c.suggestRow(ctx, row)
				resultCh <- rowResult{suggestion: suggestion, attempted: attempted, err: err}
			}
		}()
	}

	go func() {
		defer close(rowCh)
		for _, row := range rows {
			select {
			case rowCh <- row:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var suggestions []ports.RowSuggestion
	attempted, failed := 0, 0
	var lastErr error
	for res := range resultCh {
		if !res.attempted {
			continue
		}
		attempted++
		if res.err != nil {
			failed++
			lastErr = res.err
			continue
		}
		suggestions = append(suggestions, res.suggestion)
	}

	if attempted > 0 && failed == attempted {
		return suggestions, lastErr
	}
	return suggestions, nil
}
