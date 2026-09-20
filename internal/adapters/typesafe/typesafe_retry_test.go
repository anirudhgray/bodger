package typesafe_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/anirudhgray/bodger/internal/adapters/typesafe"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// TestSuggest_Retry429ThenSuccess is ADR-0015's "a 429 then success" case:
// the first attempt is throttled, the second succeeds, and Suggest must
// return the successful answer rather than the 429 error.
func TestSuggest_Retry429ThenSuccess(t *testing.T) {
	var calls int32
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return jsonResponseWithHeader(429, `{}`, "Retry-After", "0"), nil
		}
		return jsonResponse(200, `{"answers": {"category": {"choice": "cat-food", "confidence": 0.8}}}`), nil
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	got, _, err := p.Suggest(context.Background(), []ports.SuggestionRow{categoryRow("rec-1")})
	if err != nil {
		t.Fatalf("Suggest: %v, want success after one retry", err)
	}
	if len(got) != 1 || got[0].CategoryID != "cat-food" {
		t.Fatalf("got %+v, want one suggestion with CategoryID=cat-food", got)
	}
	if calls != 2 {
		t.Fatalf("got %d HTTP calls, want exactly 2 (one 429, one success)", calls)
	}
}

// TestSuggest_401NotRetried pins that a 401 is never retried: the fake
// transport must be called exactly once, and the call must fail
// immediately rather than after 3 attempts' worth of backoff.
func TestSuggest_401NotRetried(t *testing.T) {
	var calls int32
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(401, `{}`), nil
	})
	p := typesafe.New("bad-key", "http://example.invalid", client)

	_, _, err := p.Suggest(context.Background(), []ports.SuggestionRow{categoryRow("rec-1")})
	wantErrCode(t, err, errs.Unavailable)
	if calls != 1 {
		t.Fatalf("got %d HTTP calls for a 401, want exactly 1 (401 must never be retried)", calls)
	}
}

// TestSuggest_403NotRetried mirrors the 401 case: also never retried.
func TestSuggest_403NotRetried(t *testing.T) {
	var calls int32
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(403, `{}`), nil
	})
	p := typesafe.New("bad-key", "http://example.invalid", client)

	_, _, err := p.Suggest(context.Background(), []ports.SuggestionRow{categoryRow("rec-1")})
	wantErrCode(t, err, errs.Unavailable)
	if calls != 1 {
		t.Fatalf("got %d HTTP calls for a 403, want exactly 1 (403 must never be retried)", calls)
	}
}

// TestSuggest_422NotRetried: 422 is Internal and, per ADR-0015, also never
// retried — a malformed question won't fix itself on a second attempt.
func TestSuggest_422NotRetried(t *testing.T) {
	var calls int32
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponse(422, `{}`), nil
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	_, _, err := p.Suggest(context.Background(), []ports.SuggestionRow{categoryRow("rec-1")})
	wantErrCode(t, err, errs.Internal)
	if calls != 1 {
		t.Fatalf("got %d HTTP calls for a 422, want exactly 1 (422 must never be retried)", calls)
	}
}

// TestSuggest_RetryCeiling pins "at most 2 retries": a row that fails
// retryably on every attempt must be called exactly 3 times (the initial
// attempt plus 2 retries), never more.
func TestSuggest_RetryCeiling(t *testing.T) {
	var calls int32
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return jsonResponseWithHeader(429, `{}`, "Retry-After", "0"), nil
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	_, _, err := p.Suggest(context.Background(), []ports.SuggestionRow{categoryRow("rec-1")})
	wantErrCode(t, err, errs.Unavailable)
	if calls != 3 {
		t.Fatalf("got %d HTTP calls, want exactly 3 (1 initial + 2 retries, then give up)", calls)
	}
}

// TestSuggest_RetryCeiling_ConnectionFailures pins the same ceiling for
// transport-level failures, not just HTTP status codes.
func TestSuggest_RetryCeiling_ConnectionFailures(t *testing.T) {
	var calls int32
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return nil, context.DeadlineExceeded
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	_, _, err := p.Suggest(context.Background(), []ports.SuggestionRow{categoryRow("rec-1")})
	wantErrCode(t, err, errs.Unavailable)
	if calls != 3 {
		t.Fatalf("got %d HTTP calls, want exactly 3 (1 initial + 2 retries)", calls)
	}
}
