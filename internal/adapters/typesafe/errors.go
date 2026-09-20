package typesafe

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// mapStatusError maps a terminal (non-retried, or retries-exhausted) HTTP
// status onto bodger's error model, per ADR-0015's error table. It never
// receives, and therefore can never wrap, the request or its headers — the
// only input is the status code, which carries no credential.
//
// 401/403 are deliberately Unavailable, not Unauthenticated: the actor
// reviewing an import is authenticated. It's the operator's instance
// credential that's wrong or out of credit, which ADR-0011's
// Unauthenticated ("you need to sign in") would misdescribe. 422 is
// Internal, not InvalidInput: a malformed question is bodger constructing
// the request wrongly, not the user's data.
func mapStatusError(status int) *errs.Error {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return errs.New(errs.Unavailable).
			Explain("typesafe.ai rejected this instance's credential (status %d)", status).
			With("reason", "credential_rejected")
	case status == http.StatusTooManyRequests:
		return errs.New(errs.Unavailable).
			Explain("typesafe.ai is rate-limiting this instance (status 429)").
			With("reason", "throttled")
	case status == http.StatusUnprocessableEntity:
		return errs.New(errs.Internal).
			Explain("typesafe.ai rejected the request as malformed (status 422) — this is a bodger bug, not a problem with your data")
	case status == 529 || status >= 500:
		return errs.New(errs.Unavailable).
			Explain("typesafe.ai is unreachable (status %d)", status).
			With("reason", "provider_unreachable")
	default:
		return errs.New(errs.Internal).
			Explain("typesafe.ai returned an unexpected status %d", status)
	}
}

// mapTransportError maps a transport-level failure — DNS, connection
// refused, a request timeout — onto the same "provider_unreachable"
// reason a 5xx gets, per ADR-0015's error table. cause is the underlying
// net/http error (a *url.Error wrapping a *net.OpError, typically); it is
// wrapped for the log's cause chain (ADR-0011) and never includes the
// request or its headers, only net/http's own error text.
func mapTransportError(cause error) *errs.Error {
	return errs.New(errs.Unavailable).
		Explain("could not reach the suggestion provider").
		With("reason", "provider_unreachable").
		Wrap(cause)
}
