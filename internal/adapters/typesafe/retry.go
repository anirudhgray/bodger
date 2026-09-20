package typesafe

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// maxRetries is ADR-0015's "at most 2 retries" — three attempts in total
// for a request that keeps failing retryably.
const maxRetries = 2

// baseRetryDelay and maxRetryDelay bound the exponential backoff: the
// first retry waits around baseRetryDelay, doubling each attempt after,
// never exceeding maxRetryDelay regardless of how many attempts or what
// the server asked for — ADR-0015's "capped so one user action cannot
// hang".
const (
	baseRetryDelay = 250 * time.Millisecond
	maxRetryDelay  = 5 * time.Second
)

// retryableStatus reports whether status is one ADR-0015 allows retrying:
// 429, 529, or any 5xx. 401, 403, and 422 are deliberately excluded —
// "they will not improve" by asking again.
func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == 529 || status >= 500
}

// backoffDelay computes how long to wait before the next attempt, given
// how many attempts have already been made (attempt, zero-based) and any
// server-supplied retry delay (retryAfter, zero if none was given).
//
// It uses "equal jitter": compute the exponential delay (or the server's
// requested delay if that's larger), cap it, then return half of that plus
// a random amount up to the other half. That guarantees the result never
// exceeds the cap while still spreading retries out, per ADR-0015's
// "exponential backoff with jitter, honouring a server-supplied retry
// delay, capped".
func backoffDelay(attempt int, retryAfter time.Duration) time.Duration {
	exp := baseRetryDelay * time.Duration(1<<uint(attempt)) //nolint:gosec // attempt is bounded by maxRetries
	if retryAfter > exp {
		exp = retryAfter
	}
	if exp > maxRetryDelay {
		exp = maxRetryDelay
	}
	half := exp / 2
	if half <= 0 {
		return exp
	}
	return half + time.Duration(rand.Int63n(int64(half)+1)) //nolint:gosec // jitter, not a security-sensitive value
}

// parseRetryAfter reads a Retry-After header value, per RFC 9110 either an
// integer number of seconds or an HTTP-date. It returns 0 (no preference)
// if the header is absent or unparseable in either form.
func parseRetryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(v); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

// sleepCtx waits for d, or returns ctx's error early if ctx is done first —
// so a retry never outlives the caller's own deadline or cancellation.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// doWithRetry issues the request buildReq constructs, retrying per
// ADR-0015's retry policy: at most maxRetries additional attempts, only
// for a retryable status or a transport-level failure, with capped
// exponential backoff plus jitter honouring any server-supplied delay.
// buildReq is called again for every attempt because an *http.Request's
// body can only be read once.
//
// On success (a response bodger will map itself, including a terminal
// non-2xx after retries are exhausted), the caller owns the returned
// response and must close its body. On failure it returns a wrapped
// errs.Error — never the request or its headers, per ADR-0015's
// credential-in-logs rule.
func (c *Client) doWithRetry(ctx context.Context, buildReq func() (*http.Request, error)) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return nil, errs.New(errs.Unavailable).
				Explain("could not reach the suggestion provider").
				With("reason", "provider_unreachable").
				Wrap(ctx.Err())
		}

		req, err := buildReq()
		if err != nil {
			return nil, errs.New(errs.Internal).
				Explain("could not build a request to the suggestion provider").
				Wrap(err)
		}

		resp, doErr := c.httpClient.Do(req)
		if doErr != nil {
			if attempt < maxRetries {
				if sleepErr := sleepCtx(ctx, backoffDelay(attempt, 0)); sleepErr != nil {
					return nil, errs.New(errs.Unavailable).
						Explain("could not reach the suggestion provider").
						With("reason", "provider_unreachable").
						Wrap(sleepErr)
				}
				continue
			}
			return nil, mapTransportError(doErr)
		}

		if attempt < maxRetries && retryableStatus(resp.StatusCode) {
			retryAfter := parseRetryAfter(resp.Header)
			drainAndClose(resp)
			if sleepErr := sleepCtx(ctx, backoffDelay(attempt, retryAfter)); sleepErr != nil {
				return nil, errs.New(errs.Unavailable).
					Explain("could not reach the suggestion provider").
					With("reason", "provider_unreachable").
					Wrap(sleepErr)
			}
			continue
		}

		return resp, nil
	}
}
