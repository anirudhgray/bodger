package fxprovider_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/adapters/fxprovider"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// roundTripFunc lets a test fabricate an *http.Response (or a transport
// error) for any request without a real network call, per ADR-0012's "do
// not make real network calls in the test suite" and the issue's ask for
// tests against a fake/stub HTTP transport.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func clientReturning(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func mustDate(t *testing.T, y int, m time.Month, d int) domain.Date {
	t.Helper()
	date, err := domain.NewDate(y, m, d)
	if err != nil {
		t.Fatalf("NewDate(%d, %d, %d): %v", y, m, d, err)
	}
	return date
}

func wantErrCode(t *testing.T, err error, want errs.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("got nil error, want code %q", want)
	}
	var e *errs.Error
	if ce, ok := err.(*errs.Error); ok {
		e = ce
	} else {
		t.Fatalf("got error of type %T (%v), want *errs.Error with code %q", err, err, want)
	}
	if e.Code != want {
		t.Fatalf("got code %q, want %q (message: %s)", e.Code, want, e.Message)
	}
}

// --- 1. status-code -> error mapping table -------------------------------

func TestFetchRate_StatusCodeMapping(t *testing.T) {
	requested := mustDate(t, 2026, time.January, 15)

	t.Run("200 exact date match returns the rate", func(t *testing.T) {
		client := clientReturning(func(r *http.Request) (*http.Response, error) {
			return jsonResponse(200, `{"date":"2026-01-15","base":"INR","quote":"USD","rate":0.01109}`), nil
		})
		p := fxprovider.New("http://example.invalid", client)

		got, err := p.FetchRate(context.Background(), "INR", "USD", requested)
		if err != nil {
			t.Fatalf("FetchRate: %v", err)
		}
		if !got.Date.Equal(requested) {
			t.Errorf("Date = %s, want %s", got.Date, requested)
		}
		if got.Rate.Value().String() != "0.01109" {
			t.Errorf("Rate = %s, want 0.01109", got.Rate.Value())
		}
	})

	t.Run("200 walked-back date is still success, dated at the row", func(t *testing.T) {
		client := clientReturning(func(r *http.Request) (*http.Response, error) {
			return jsonResponse(200, `{"date":"2026-01-14","base":"INR","quote":"JPY","rate":1.42}`), nil
		})
		p := fxprovider.New("http://example.invalid", client)

		got, err := p.FetchRate(context.Background(), "INR", "JPY", requested)
		if err != nil {
			t.Fatalf("FetchRate: %v", err)
		}
		want := mustDate(t, 2026, time.January, 14)
		if !got.Date.Equal(want) {
			t.Errorf("Date = %s, want %s (the row's date, not the requested one)", got.Date, want)
		}
	})

	t.Run("404 maps to NotFound, not Unavailable", func(t *testing.T) {
		client := clientReturning(func(r *http.Request) (*http.Response, error) {
			return jsonResponse(404, `{"message":"not found"}`), nil
		})
		p := fxprovider.New("http://example.invalid", client)

		_, err := p.FetchRate(context.Background(), "EUR", "INR", requested)
		wantErrCode(t, err, errs.NotFound)
	})

	t.Run("422 maps to InvalidInput", func(t *testing.T) {
		client := clientReturning(func(r *http.Request) (*http.Response, error) {
			return jsonResponse(422, `{"message":"not supported"}`), nil
		})
		p := fxprovider.New("http://example.invalid", client)

		_, err := p.FetchRate(context.Background(), "EUR", "XYZ", requested)
		wantErrCode(t, err, errs.InvalidInput)
	})

	t.Run("500 maps to Unavailable", func(t *testing.T) {
		client := clientReturning(func(r *http.Request) (*http.Response, error) {
			return jsonResponse(500, `internal error`), nil
		})
		p := fxprovider.New("http://example.invalid", client)

		_, err := p.FetchRate(context.Background(), "EUR", "USD", requested)
		wantErrCode(t, err, errs.Unavailable)
	})

	t.Run("timeout maps to Unavailable", func(t *testing.T) {
		client := clientReturning(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("simulated timeout: %w", context.DeadlineExceeded)
		})
		p := fxprovider.New("http://example.invalid", client)

		_, err := p.FetchRate(context.Background(), "EUR", "USD", requested)
		wantErrCode(t, err, errs.Unavailable)
	})

	t.Run("DNS failure maps to Unavailable", func(t *testing.T) {
		client := clientReturning(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("lookup example.invalid: no such host")
		})
		p := fxprovider.New("http://example.invalid", client)

		_, err := p.FetchRate(context.Background(), "EUR", "USD", requested)
		wantErrCode(t, err, errs.Unavailable)
	})

	t.Run("connection refused maps to Unavailable", func(t *testing.T) {
		client := clientReturning(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("dial tcp 127.0.0.1:1: connect: connection refused")
		})
		p := fxprovider.New("http://example.invalid", client)

		_, err := p.FetchRate(context.Background(), "EUR", "USD", requested)
		wantErrCode(t, err, errs.Unavailable)
	})
}
