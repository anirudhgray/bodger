package logging_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/platform/logging"
)

func TestNew_WritesStructuredJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(&buf, slog.LevelInfo)

	logger.Info("account created", "account_id", "acct-000001")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("log output is not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if record["msg"] != "account created" {
		t.Errorf("msg = %v, want %q", record["msg"], "account created")
	}
	if record["account_id"] != "acct-000001" {
		t.Errorf("account_id = %v, want %q", record["account_id"], "acct-000001")
	}
}

func TestNew_RespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(&buf, slog.LevelWarn)

	logger.Info("should not appear")
	if buf.Len() != 0 {
		t.Errorf("Info line was written below the configured Warn level: %s", buf.String())
	}

	logger.Warn("should appear")
	if buf.Len() == 0 {
		t.Error("Warn line was not written at the configured Warn level")
	}
}

// TestNew_RendersErrsErrorViaLogValue is a smaller integration check that
// this package's constructor produces a logger through which an
// *errs.Error's LogValue (internal/platform/errs) is dispatched
// automatically — the exhaustive cause-non-leakage assertions live in
// internal/platform/errs, this just confirms the wiring works end to end.
func TestNew_RendersErrsErrorViaLogValue(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.New(&buf, slog.LevelInfo)

	cause := errors.New("driver: connection refused")
	err := errs.New(errs.Unavailable).Wrap(cause)

	logger.Error("startup check failed", "error", err)

	out := buf.String()
	if !strings.Contains(out, "connection refused") {
		t.Errorf("log output is missing the wrapped cause: %s", out)
	}
	if !strings.Contains(out, string(errs.Unavailable)) {
		t.Errorf("log output is missing the error code: %s", out)
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    slog.Level
		wantErr bool
	}{
		{name: "debug", in: "debug", want: slog.LevelDebug},
		{name: "info", in: "info", want: slog.LevelInfo},
		{name: "warn", in: "warn", want: slog.LevelWarn},
		{name: "warning synonym", in: "warning", want: slog.LevelWarn},
		{name: "error", in: "error", want: slog.LevelError},
		{name: "case-insensitive", in: "ERROR", want: slog.LevelError},
		{name: "unrecognised", in: "verbose", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := logging.ParseLevel(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) returned no error, want one", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLevel(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
