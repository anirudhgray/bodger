package typesafe_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/anirudhgray/bodger/internal/adapters/typesafe"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// TestSuggest_ErrorMapping is one case per row of ADR-0015's error table.
// Every row here uses a single-row request that always fails the same way,
// so retries (covered separately in typesafe_retry_test.go) don't matter —
// only status codes that ADR-0015 says are never retried are used for the
// 401/403/422 cases, so this test isn't slowed by backoff.
func TestSuggest_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		respond    func(r *http.Request) (*http.Response, error)
		wantCode   errs.Code
		wantReason string // "" means no reason detail is asserted
	}{
		{
			name:       "401 unauthorized maps to Unavailable/credential_rejected",
			respond:    func(r *http.Request) (*http.Response, error) { return jsonResponse(401, `{}`), nil },
			wantCode:   errs.Unavailable,
			wantReason: "credential_rejected",
		},
		{
			name:       "403 forbidden maps to Unavailable/credential_rejected",
			respond:    func(r *http.Request) (*http.Response, error) { return jsonResponse(403, `{}`), nil },
			wantCode:   errs.Unavailable,
			wantReason: "credential_rejected",
		},
		{
			name:       "422 unprocessable maps to Internal",
			respond:    func(r *http.Request) (*http.Response, error) { return jsonResponse(422, `{}`), nil },
			wantCode:   errs.Internal,
			wantReason: "",
		},
		{
			name: "connection refused maps to Unavailable/provider_unreachable",
			respond: func(r *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("dial tcp 127.0.0.1:1: connect: connection refused")
			},
			wantCode:   errs.Unavailable,
			wantReason: "provider_unreachable",
		},
		{
			name: "DNS failure maps to Unavailable/provider_unreachable",
			respond: func(r *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("lookup example.invalid: no such host")
			},
			wantCode:   errs.Unavailable,
			wantReason: "provider_unreachable",
		},
		{
			name: "timeout maps to Unavailable/provider_unreachable",
			respond: func(r *http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			},
			wantCode:   errs.Unavailable,
			wantReason: "provider_unreachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// These all fail on every attempt, so with the retry policy's
			// maxRetries=2 the fake transport gets called 3 times for
			// retryable outcomes (there are none among these — 401/403/422
			// are never retried) and once for the transport-error cases,
			// since transport errors *are* retryable but this test wants
			// to isolate the mapping, not the retry ceiling.
			//
			// Only 401/403/422 are asserted with a single call; the
			// transport-error and timeout cases are allowed to retry
			// (that's correct per ADR-0015) and are asserted purely on the
			// final mapped code/reason.
			client := clientReturning(tt.respond)
			p := typesafe.New("test-key", "http://example.invalid", client)

			_, outcome, err := p.Suggest(context.Background(), []ports.SuggestionRow{categoryRow("rec-1")})
			wantErrCode(t, err, tt.wantCode)
			if tt.wantReason != "" {
				var e *errs.Error
				if !errors.As(err, &e) {
					t.Fatalf("could not extract *errs.Error from %v", err)
				}
				if got, _ := e.Details["reason"].(string); got != tt.wantReason {
					t.Errorf("reason detail = %q, want %q", got, tt.wantReason)
				}
				// The same reason must also reach SuggestOutcome, not
				// only the error's own Details — this is what a partial
				// failure (unlike this single-row total failure) has to
				// rely on, since its error return stays nil.
				if outcome.FailureReason != tt.wantReason {
					t.Errorf("outcome.FailureReason = %q, want %q", outcome.FailureReason, tt.wantReason)
				}
			}
			if outcome.FailedRows != 1 {
				t.Errorf("outcome.FailedRows = %d, want 1", outcome.FailedRows)
			}
		})
	}
}

func TestSuggest_530Overloaded_And5xx_MapToUnavailableProviderUnreachable(t *testing.T) {
	for _, status := range []int{529, 500, 502, 503} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			client := clientReturning(func(r *http.Request) (*http.Response, error) {
				return jsonResponse(status, `{}`), nil
			})
			p := typesafe.New("test-key", "http://example.invalid", client)

			_, outcome, err := p.Suggest(context.Background(), []ports.SuggestionRow{categoryRow("rec-1")})
			wantErrCode(t, err, errs.Unavailable)
			var e *errs.Error
			if errors.As(err, &e) {
				if got, _ := e.Details["reason"].(string); got != "provider_unreachable" {
					t.Errorf("reason detail = %q, want provider_unreachable", got)
				}
			}
			if outcome.FailureReason != "provider_unreachable" {
				t.Errorf("outcome.FailureReason = %q, want provider_unreachable", outcome.FailureReason)
			}
		})
	}
}

func TestSuggest_429_MapsToUnavailableThrottled(t *testing.T) {
	client := clientReturning(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(429, `{}`), nil
	})
	p := typesafe.New("test-key", "http://example.invalid", client)

	_, outcome, err := p.Suggest(context.Background(), []ports.SuggestionRow{categoryRow("rec-1")})
	wantErrCode(t, err, errs.Unavailable)
	var e *errs.Error
	if errors.As(err, &e) {
		if got, _ := e.Details["reason"].(string); got != "throttled" {
			t.Errorf("reason detail = %q, want throttled", got)
		}
	}
	if outcome.FailureReason != "throttled" {
		t.Errorf("outcome.FailureReason = %q, want throttled", outcome.FailureReason)
	}
}
