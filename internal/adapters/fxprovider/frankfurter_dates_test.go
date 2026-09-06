package fxprovider_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/adapters/fxprovider"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// TestFetchRate_RejectsForwardDatedResponse guards ADR-0012's "reject a
// returned date later than the requested one... it needs a test in the
// adapter suite that fails on that specific substitution, not just a
// comment." A provider that answers a request for 2026-01-15 with a row
// dated 2026-01-16 must produce a caller-facing error, never a silently
// stored forward-dated rate.
func TestFetchRate_RejectsForwardDatedResponse(t *testing.T) {
	requested := mustDate(t, 2026, time.January, 15)

	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `{"date":"2026-01-16","base":"EUR","quote":"USD","rate":1.05}`), nil
	})
	p := fxprovider.New("http://example.invalid", client)

	got, err := p.FetchRate(context.Background(), "EUR", "USD", requested)
	if err == nil {
		t.Fatalf("FetchRate returned a forward-dated rate with no error: %+v", got)
	}
	wantErrCode(t, err, errs.Internal)
}

// TestFetchRange_RejectsRowsDatedAfterTo applies the same rule to the
// range endpoint: no row may be dated after the range's upper bound.
func TestFetchRange_RejectsRowsDatedAfterTo(t *testing.T) {
	from := mustDate(t, 2026, time.January, 10)
	to := mustDate(t, 2026, time.January, 15)

	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `[
			{"date":"2026-01-12","base":"EUR","quote":"USD","rate":1.04},
			{"date":"2026-01-16","base":"EUR","quote":"USD","rate":1.05}
		]`), nil
	})
	p := fxprovider.New("http://example.invalid", client)

	got, err := p.FetchRange(context.Background(), "EUR", "USD", from, to)
	if err == nil {
		t.Fatalf("FetchRange returned rows including a forward-dated one with no error: %+v", got)
	}
	wantErrCode(t, err, errs.Internal)
}
