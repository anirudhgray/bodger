package cli_test

import (
	"strings"
	"testing"

	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// TestConfigReportingCurrency_GetUnset checks the "never configured" state
// (issue #132's GetReportingCurrency returning "" with no error): the
// --json response says is_set is false, and the plain-text message names
// the instance's own default rather than leaving the user to guess it.
func TestConfigReportingCurrency_GetUnset(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))

	var got struct {
		Currency string `json:"currency"`
		IsSet    bool   `json:"is_set"`
	}
	decodeData(t, mustRun(t, factory, "config", "reporting-currency", "get", "--json"), &got)
	if got.IsSet || got.Currency != "" {
		t.Errorf("got %+v, want unset", got)
	}

	text := mustRun(t, factory, "config", "reporting-currency", "get")
	if !strings.Contains(text, "USD") {
		t.Errorf("text = %q, want it to name the instance default (USD)", text)
	}
}

// TestConfigReportingCurrency_SetThenGet checks the round trip: set,
// followed by get reflecting exactly what was set.
func TestConfigReportingCurrency_SetThenGet(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))

	var status struct {
		Message string `json:"message"`
	}
	decodeData(t, mustRun(t, factory, "config", "reporting-currency", "set", "EUR", "--json"), &status)
	if !strings.Contains(status.Message, "EUR") {
		t.Errorf("status message = %q, want it to mention EUR", status.Message)
	}

	var got struct {
		Currency string `json:"currency"`
		IsSet    bool   `json:"is_set"`
	}
	decodeData(t, mustRun(t, factory, "config", "reporting-currency", "get", "--json"), &got)
	if !got.IsSet || got.Currency != "EUR" {
		t.Errorf("got %+v, want EUR, set", got)
	}
}

// TestConfigReportingCurrency_SetRejectsUnknownCurrency checks this
// package forwards SetReportingCurrency's own InvalidInput rather than
// validating currency codes itself.
func TestConfigReportingCurrency_SetRejectsUnknownCurrency(t *testing.T) {
	factory := newTestFactory(t, mustFrozen(t))
	_, _, err := run(t, factory, "config", "reporting-currency", "set", "NOPE")
	wantErrCode(t, err, errs.InvalidInput)
}
