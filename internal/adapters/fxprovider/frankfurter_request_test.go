package fxprovider_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/adapters/fxprovider"
)

// TestFetchRate_BuildsExpectedRequest pins the request shape ADR-0012
// documents for a single-date lookup: GET /v2/rate/{base}/{quote}?date=.
func TestFetchRate_BuildsExpectedRequest(t *testing.T) {
	var gotURL *url.URL
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		return jsonResponse(200, `{"date":"2026-01-15","base":"INR","quote":"USD","rate":0.01109}`), nil
	})
	p := fxprovider.New("https://api.frankfurter.dev", client)

	_, err := p.FetchRate(context.Background(), "INR", "USD", mustDate(t, 2026, time.January, 15))
	if err != nil {
		t.Fatalf("FetchRate: %v", err)
	}
	if gotURL == nil {
		t.Fatal("no request was made")
	}
	if gotURL.Path != "/v2/rate/INR/USD" {
		t.Errorf("path = %s, want /v2/rate/INR/USD", gotURL.Path)
	}
	if gotURL.Query().Get("date") != "2026-01-15" {
		t.Errorf("date query param = %s, want 2026-01-15", gotURL.Query().Get("date"))
	}
}

// TestNew_Defaults confirms New's exported defaults match ADR-0012's
// fx.provider_base_url default and constructs successfully when the
// caller supplies neither a base URL nor a client -- while still
// accepting an explicit override for a self-hosted instance or a test
// double (every other test in this package does exactly that).
func TestNew_Defaults(t *testing.T) {
	if fxprovider.DefaultBaseURL != "https://api.frankfurter.dev" {
		t.Errorf("DefaultBaseURL = %s, want https://api.frankfurter.dev", fxprovider.DefaultBaseURL)
	}
	if fxprovider.DefaultTimeout < 10*time.Second {
		t.Errorf("DefaultTimeout = %s, want comfortably above Frankfurter's ~5s P95 latency", fxprovider.DefaultTimeout)
	}
	if p := fxprovider.New("", nil); p == nil {
		t.Fatal("New(\"\", nil) returned nil")
	}
}
