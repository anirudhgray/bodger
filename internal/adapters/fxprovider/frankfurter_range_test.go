package fxprovider_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/adapters/fxprovider"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// TestFetchRange_SurfacesEveryDistinctDate covers ADR-0012's "a batch
// request for one date routinely returns rows with several different
// dates in it" -- generalized to a range fetch. FetchRange must hand back
// every row the provider returned, each carrying its own date, rather
// than collapsing to a single value or picking one date to stand in for
// the rest.
func TestFetchRange_SurfacesEveryDistinctDate(t *testing.T) {
	from := mustDate(t, 2026, time.January, 10)
	to := mustDate(t, 2026, time.January, 17)

	var gotURL *url.URL
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL
		return jsonResponse(200, `[
			{"date":"2026-01-10","base":"INR","quote":"USD","rate":0.01109},
			{"date":"2026-01-13","base":"INR","quote":"USD","rate":0.01111},
			{"date":"2026-01-16","base":"INR","quote":"USD","rate":0.01105}
		]`), nil
	})
	p := fxprovider.New("http://example.invalid", client)

	got, err := p.FetchRange(context.Background(), "INR", "USD", from, to)
	if err != nil {
		t.Fatalf("FetchRange: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3 -- every distinct returned date should be surfaced", len(got))
	}

	wantDates := []string{"2026-01-10", "2026-01-13", "2026-01-16"}
	wantRates := []string{"0.01109", "0.01111", "0.01105"}
	for i, row := range got {
		if row.Date.String() != wantDates[i] {
			t.Errorf("row %d Date = %s, want %s", i, row.Date, wantDates[i])
		}
		if row.Rate.Value().String() != wantRates[i] {
			t.Errorf("row %d Rate = %s, want %s", i, row.Rate.Value(), wantRates[i])
		}
	}

	// One request per pair, using the from/to time-series primitive --
	// not a loop over individual dates (ADR-0012).
	if gotURL == nil {
		t.Fatal("no request was made")
	}
	q := gotURL.Query()
	if q.Get("from") != "2026-01-10" || q.Get("to") != "2026-01-17" {
		t.Errorf("query = %s, want from=2026-01-10&to=2026-01-17", gotURL.RawQuery)
	}
}

// TestFetchRange_NoDataMapsToNotFound covers the empty-array case: a
// range with no coverage at all is "no data for this pair", not a
// zero-length success.
func TestFetchRange_NoDataMapsToNotFound(t *testing.T) {
	from := mustDate(t, 1950, time.January, 1)
	to := mustDate(t, 1950, time.January, 31)

	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(200, `[]`), nil
	})
	p := fxprovider.New("http://example.invalid", client)

	_, err := p.FetchRange(context.Background(), "EUR", "USD", from, to)
	wantErrCode(t, err, errs.NotFound)
}
