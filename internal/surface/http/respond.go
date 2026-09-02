package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// actorID is every request's acting user. There is no authentication yet
// (M1, ADR-0006): every request acts as the single seeded user, the same
// way internal/surface/cli's commands do (see its resolveDefaultAccount).
// A real credential replaces this call in M2 — nothing else in this
// package needs to change when it does, since every handler already
// threads ActorID through exactly this one place.
func actorID() string {
	return ports.SeededUserID
}

// dataEnvelope is the one stable shape every successful JSON response in
// this package uses — {"data": ...} — mirroring internal/surface/cli's
// --json envelope so a client (or a person comparing the two surfaces)
// sees the same shape either way.
type dataEnvelope struct {
	Data any `json:"data"`
}

// errorEnvelope is dataEnvelope's error counterpart. *errs.Error already
// implements json.Marshaler (internal/platform/errs) with the wire shape
// ADR-0011 specifies — this only adds the "error" wrapper key.
type errorEnvelope struct {
	Error *errs.Error `json:"error"`
}

// writeJSON encodes v as the response body with the given status code and
// the standard JSON content type.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// respond writes data inside dataEnvelope with the given status code —
// every handler's success path funnels through this, the same way
// internal/surface/cli's render funnels every command's success path
// through one function.
func respond(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, dataEnvelope{Data: data})
}

// respondError renders err the way this surface reports a failure: an
// *errs.Error's registered HTTP status (issue #4's "a surface looks its
// code up here rather than choosing a status itself" — docs/architecture.md
// §2) and its safe, user-facing JSON body. Every error internal/app
// returns is already an *errs.Error; the errors.As fallback exists only
// for a genuinely unexpected error type reaching this far (a programming
// mistake, not a user-facing case), which is wrapped as Internal rather
// than leaking whatever it actually says.
//
// Before any of that, h.logger logs e's full cause chain — logger.Error
// dispatches to (*errs.Error).LogValue on its own
// (internal/platform/logging's doc comment) — so the detail this
// function is about to discard from the response (a driver message, a
// wrapped fmt.Errorf) is captured somewhere a self-hoster can find it
// rather than lost the moment this function returns (ADR-0011; issue
// #43).
func (h *handlers) respondError(w http.ResponseWriter, err error) {
	var e *errs.Error
	if !errors.As(err, &e) {
		e = errs.New(errs.Internal).Wrap(err)
	}
	if h.logger != nil {
		h.logger.Error("request failed", "error", e)
	}
	writeJSON(w, e.HTTPStatus(), errorEnvelope{Error: e})
}

// decodeJSON decodes r's body into dst, returning a rendered *errs.Error
// (InvalidInput) if the body isn't valid JSON for dst's shape. This is a
// transport-level check — "is this parseable at all" — not a validation
// decision: every field's actual validity is still decided by the
// application layer once the command reaches it.
func decodeJSON(r *http.Request, dst any) *errs.Error {
	if r.Body == nil {
		return errs.New(errs.InvalidInput).Explain("A request body is required.")
	}
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		return errs.New(errs.InvalidInput).Explain("The request body isn't valid JSON.").Wrap(err)
	}
	return nil
}

// decodeTags normalises every raw tag via normalize.Tag — the one command
// field this package is allowed to construct itself at decode time
// (docs/decisions/0005-shared-application-layer.md: tag normalisation is
// pure and needs no repository lookup or clock). Every other field on
// every command struct in this package passes through as the raw string
// or strings the client sent.
func decodeTags(raw []string) ([]string, *errs.Error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		t, err := normalize.Tag(r)
		if err != nil {
			var e *errs.Error
			if errors.As(err, &e) {
				return nil, e
			}
			return nil, errs.New(errs.InvalidInput).Explain("%q isn't a usable tag.", r).Field("tags").Wrap(err)
		}
		out = append(out, t.String())
	}
	return out, nil
}
