package http_test

import (
	"net/http"
	"testing"
	"time"
)

// TestBalances_CurrencyAndPolicyRenderProvenanceAndUnconverted is issue
// #137's balance-conversion surface, end to end: a USD account converts to
// itself as an identity (no rate to show), an INR account converts via a
// stored rate with full ADR-0004 provenance rendered inline, and a EUR
// account with no stored rate is listed under "unconverted" with its
// reason rather than silently dropped from the response — the
// HTTP-surface equivalent of
// internal/surface/cli/cli_test.go's
// TestBalance_CurrencyAndPolicyRenderProvenanceAndUnconverted.
func TestBalances_CurrencyAndPolicyRenderProvenanceAndUnconverted(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	provider := newFakeFxProvider()
	srv := newTestServerWithFxProvider(t, frozenAt, "UTC", provider)

	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{
		"name": "Wallet", "type": "cash", "currency": "INR", "opening_balance": "1000",
	})
	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{
		"name": "Checking", "type": "bank", "currency": "USD", "opening_balance": "500",
	})
	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{
		"name": "Euro Account", "type": "bank", "currency": "EUR", "opening_balance": "200",
	})

	provider.setRate(t, "INR", "USD", "0.0115", mustFxTestDate(t, 2026, time.August, 14))
	do(t, srv, http.MethodPost, "/api/v1/fx/rates/fetch", map[string]any{"pairs": []string{"INR"}})

	status, decoded := do(t, srv, http.MethodGet, "/api/v1/balances?currency=USD&policy=current", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %+v", status, decoded)
	}
	data := dataOf(t, decoded)
	balances, _ := data["balances"].([]any)

	byAccount := map[string]map[string]any{}
	for _, b := range balances {
		row := b.(map[string]any)
		byAccount[row["account"].(string)] = row
	}

	wallet := byAccount["Wallet"]
	converted, ok := wallet["converted"].(map[string]any)
	if !ok {
		t.Fatalf("Wallet has no converted figure: %+v", wallet)
	}
	if converted["amount"] != "11.50" || converted["currency"] != "USD" {
		t.Errorf("Wallet converted = %+v, want 11.50 USD (1000 INR @ 0.0115)", converted)
	}
	if converted["rate"] != "0.0115" || converted["rate_source"] != "fake-provider" || converted["policy"] != "current" {
		t.Errorf("Wallet converted provenance = %+v", converted)
	}
	if converted["stale"] != false {
		t.Errorf("Wallet.converted.stale = %v, want false (rate fetched at today's date)", converted["stale"])
	}

	checking := byAccount["Checking"]
	checkingConverted, ok := checking["converted"].(map[string]any)
	if !ok || checkingConverted["amount"] != "500.00" || checkingConverted["rate_source"] != "" {
		t.Errorf("Checking converted = %+v, want an identity conversion with no rate source", checking["converted"])
	}

	unconverted, _ := data["unconverted"].([]any)
	if len(unconverted) != 1 {
		t.Fatalf("unconverted = %+v, want exactly one entry", unconverted)
	}
	row := unconverted[0].(map[string]any)
	if row["account"] != "Euro Account" || row["reason"] == "" {
		t.Errorf("unconverted[0] = %+v, want Euro Account with a reason", row)
	}
}

// TestBalances_InvalidPolicyIs422 checks that an unrecognized policy value
// surfaces the app layer's own InvalidInput as a 422, without this
// handler pre-validating the policy enum itself.
func TestBalances_InvalidPolicyIs422(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Cash", "type": "cash", "currency": "USD"})

	status, decoded := do(t, srv, http.MethodGet, "/api/v1/balances?currency=USD&policy=yesterday", nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %+v", status, decoded)
	}
	if code := errorCodeOf(t, decoded); code != "invalid_input" {
		t.Errorf("error code = %q, want invalid_input", code)
	}
}
