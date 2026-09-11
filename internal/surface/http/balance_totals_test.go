package http_test

import (
	"net/http"
	"testing"
	"time"
)

// TestBalanceTotals_OverallByCategoryByCurrency exercises GET
// /api/v1/balances/totals end to end: an INR account and a USD account
// (converted via a stored rate into the resolved reporting currency), and
// a EUR account with no stored rate — reported under "unconverted" and
// excluded from "overall"/"by_category", but still summed raw under
// "by_currency" (issue #195).
func TestBalanceTotals_OverallByCategoryByCurrency(t *testing.T) {
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

	status, decoded := do(t, srv, http.MethodGet, "/api/v1/balances/totals?currency=USD&policy=current", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %+v", status, decoded)
	}
	data := dataOf(t, decoded)

	if data["currency"] != "USD" {
		t.Errorf("currency = %v, want USD", data["currency"])
	}
	// Overall: 1000 INR @ 0.0115 = 11.50 USD, + Checking's own 500.00 USD
	// (identity conversion) = 511.50 USD. Euro Account is excluded (no
	// rate available).
	if data["overall"] != "511.50" {
		t.Errorf("overall = %v, want 511.50 (Euro Account excluded, no rate)", data["overall"])
	}

	byCategory, _ := data["by_category"].([]any)
	categoryByKind := map[string]string{}
	for _, row := range byCategory {
		r := row.(map[string]any)
		categoryByKind[r["kind"].(string)] = r["amount"].(string)
	}
	// cash (Wallet, converted) = 11.50; bank (Checking converted + Euro
	// Account excluded) = 500.00.
	if categoryByKind["cash"] != "11.50" {
		t.Errorf("by_category[cash] = %q, want 11.50", categoryByKind["cash"])
	}
	if categoryByKind["bank"] != "500.00" {
		t.Errorf("by_category[bank] = %q, want 500.00 (Euro Account excluded)", categoryByKind["bank"])
	}

	byCurrency, _ := data["by_currency"].([]any)
	currencyTotals := map[string]string{}
	for _, row := range byCurrency {
		r := row.(map[string]any)
		currencyTotals[r["currency"].(string)] = r["amount"].(string)
	}
	if currencyTotals["INR"] != "1000.00" {
		t.Errorf("by_currency[INR] = %q, want 1000.00 (raw, unconverted)", currencyTotals["INR"])
	}
	if currencyTotals["USD"] != "500.00" {
		t.Errorf("by_currency[USD] = %q, want 500.00 (raw, unconverted)", currencyTotals["USD"])
	}
	if currencyTotals["EUR"] != "200.00" {
		t.Errorf("by_currency[EUR] = %q, want 200.00 (raw, still present despite no rate)", currencyTotals["EUR"])
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

// TestBalanceTotals_BlankCurrencyResolvesToTheInstanceDefault checks that
// leaving "currency" off the query resolves through ADR-0004's ladder
// (Service.resolveBalanceTotalsCurrency) rather than erroring the way
// `report`'s own required --currency would.
func TestBalanceTotals_BlankCurrencyResolvesToTheInstanceDefault(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Cash", "type": "cash", "currency": "USD"})

	status, decoded := do(t, srv, http.MethodGet, "/api/v1/balances/totals?policy=current", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %+v", status, decoded)
	}
	data := dataOf(t, decoded)
	if data["currency"] != "USD" {
		t.Errorf("currency = %v, want USD (the instance default)", data["currency"])
	}
}

// TestBalanceTotals_InvalidPolicyIs422 checks that an unrecognized policy
// value surfaces the app layer's own InvalidInput as a 422, without this
// handler pre-validating the policy enum itself — the same contract
// TestBalances_InvalidPolicyIs422 already asserts for GET /api/v1/balances.
func TestBalanceTotals_InvalidPolicyIs422(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")
	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Cash", "type": "cash", "currency": "USD"})

	status, decoded := do(t, srv, http.MethodGet, "/api/v1/balances/totals?policy=yesterday", nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %+v", status, decoded)
	}
	if code := errorCodeOf(t, decoded); code != "invalid_input" {
		t.Errorf("error code = %q, want invalid_input", code)
	}
}
