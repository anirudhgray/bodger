package http

// This file is package http, not http_test, deliberately: respondError
// (respond.go) is unexported, and this test exercises it directly rather
// than through a full NewMux round trip, since what it needs to prove —
// that an *errs.Error's cause chain reaches the logger before the safe
// response is written — has nothing to do with routing.

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// TestRespondError_LogsInternalErrorCause proves the HTTP half of issue
// #43's gap: respondError used to discard an *errs.Error's wrapped cause
// entirely. Now it must log the full chain (ADR-0011's "detail goes to
// the log, never the user") before writing the safe JSON body — this
// checks both halves of that split in one test: the cause reaches the
// log, and never reaches the response.
func TestRespondError_LogsInternalErrorCause(t *testing.T) {
	var logBuf bytes.Buffer
	h := &handlers{logger: slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))}

	cause := errors.New("sqlite: database is locked")
	err := errs.New(errs.Internal).Explain("bodger couldn't save that.").Wrap(cause)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/transactions", nil)
	h.respondError(rec, req, err)

	if rec.Code != errs.HTTPStatus(errs.Internal) {
		t.Errorf("status = %d, want %d", rec.Code, errs.HTTPStatus(errs.Internal))
	}
	if strings.Contains(rec.Body.String(), "database is locked") {
		t.Errorf("response body leaked the internal cause: %s", rec.Body.String())
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body: %v (body: %s)", err, rec.Body.String())
	}
	if body.Error.Message != "bodger couldn't save that." {
		t.Errorf("response message = %q, want the safe explanation", body.Error.Message)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "database is locked") {
		t.Errorf("log output is missing the wrapped cause: %s", logged)
	}
	if !strings.Contains(logged, string(errs.Internal)) {
		t.Errorf("log output is missing the error code: %s", logged)
	}
	if !strings.Contains(logged, `"method":"POST"`) || !strings.Contains(logged, `"path":"/api/v1/transactions"`) {
		t.Errorf("log output is missing the failing request's method/path: %s", logged)
	}
}

// TestRespondError_NilLoggerDoesNotPanic checks the nil-logger escape
// hatch NewMux's doc comment promises (tests that don't care about
// logging, like this package's own http_test.go, pass nil rather than
// wiring one up).
func TestRespondError_NilLoggerDoesNotPanic(t *testing.T) {
	h := &handlers{logger: nil}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/does-not-exist", nil)
	h.respondError(rec, req, errs.New(errs.NotFound).Explain("No such thing."))
	if rec.Code != errs.HTTPStatus(errs.NotFound) {
		t.Errorf("status = %d, want %d", rec.Code, errs.HTTPStatus(errs.NotFound))
	}
}
