package http_test

import (
	"net/http"
	"testing"
	"time"
)

// TestReportingCurrency_GetUnset checks GET /api/v1/reporting-currency's
// "never configured" case: an empty currency and is_set=false, distinct
// from a currency that happens to render the same either way.
func TestReportingCurrency_GetUnset(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodGet, "/api/v1/reporting-currency", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %+v", status, decoded)
	}
	data := dataOf(t, decoded)
	if data["is_set"] != false {
		t.Errorf("is_set = %v, want false", data["is_set"])
	}
	if data["currency"] != "" {
		t.Errorf("currency = %v, want empty", data["currency"])
	}
}

// TestReportingCurrency_SetThenGet checks POST /api/v1/reporting-currency
// with a valid currency, then that GET reflects it back with is_set=true.
func TestReportingCurrency_SetThenGet(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/reporting-currency", map[string]any{"currency": "EUR"})
	if status != http.StatusOK {
		t.Fatalf("set status = %d, want 200: %+v", status, decoded)
	}
	if dataOf(t, decoded)["ok"] != true {
		t.Errorf("ok = %v, want true", dataOf(t, decoded)["ok"])
	}

	status, decoded = do(t, srv, http.MethodGet, "/api/v1/reporting-currency", nil)
	if status != http.StatusOK {
		t.Fatalf("get status = %d, want 200: %+v", status, decoded)
	}
	data := dataOf(t, decoded)
	if data["is_set"] != true || data["currency"] != "EUR" {
		t.Errorf("got %+v, want is_set=true currency=EUR", data)
	}
}

// TestReportingCurrency_SetInvalidCurrencyIs422 checks that an unknown
// currency code surfaces the app layer's own InvalidInput, field
// "currency" — not accepted and not silently ignored.
func TestReportingCurrency_SetInvalidCurrencyIs422(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	status, decoded := do(t, srv, http.MethodPost, "/api/v1/reporting-currency", map[string]any{"currency": "NOTREAL"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %+v", status, decoded)
	}
	if code := errorCodeOf(t, decoded); code != "invalid_input" {
		t.Errorf("error code = %q, want invalid_input", code)
	}
}
