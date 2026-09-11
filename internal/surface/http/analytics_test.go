package http_test

import (
	"net/http"
	"testing"
	"time"
)

// TestCategoryBreakdown_GroupsByTopLevelCategoryAndExcludesTransfers is
// issue #188's REST surface, exercised end to end: an outflow and an
// inflow each roll up under their own top-level category, a transfer
// between two of the actor's own accounts contributes to neither, and an
// uncategorized outflow lands in its own "Uncategorized" bucket.
func TestCategoryBreakdown_GroupsByTopLevelCategoryAndExcludesTransfers(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	srv := newTestServer(t, frozenAt, "UTC")

	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Checking", "type": "bank", "currency": "USD"})
	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Savings", "type": "bank", "currency": "USD"})
	do(t, srv, http.MethodPost, "/api/v1/categories", map[string]any{"name": "Food", "type": "expense"})
	do(t, srv, http.MethodPost, "/api/v1/categories", map[string]any{"name": "Salary", "type": "income"})

	do(t, srv, http.MethodPost, "/api/v1/transactions", map[string]any{
		"type": "outflow", "account": "Checking", "category": "Food", "amount": "50", "description": "groceries",
	})
	do(t, srv, http.MethodPost, "/api/v1/transactions", map[string]any{
		"type": "inflow", "account": "Checking", "category": "Salary", "amount": "2000", "description": "paycheck",
	})
	do(t, srv, http.MethodPost, "/api/v1/transactions", map[string]any{
		"type": "outflow", "account": "Checking", "amount": "10", "description": "uncategorized",
	})
	do(t, srv, http.MethodPost, "/api/v1/transfers", map[string]any{
		"from_account": "Checking", "to_account": "Savings", "amount": "100", "description": "move to savings",
	})

	status, decoded := do(t, srv, http.MethodGet, "/api/v1/analytics/category-breakdown?currency=USD&policy=current", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %+v", status, decoded)
	}
	data := dataOf(t, decoded)
	if data["currency"] != "USD" {
		t.Errorf("currency = %v, want USD", data["currency"])
	}
	rows, _ := data["rows"].([]any)
	byCategory := map[string]map[string]any{}
	for _, r := range rows {
		row := r.(map[string]any)
		byCategory[row["category"].(string)] = row
	}

	food, ok := byCategory["Food"]
	if !ok || food["spending"] != "50.00" {
		t.Errorf("Food row = %+v, want spending 50.00", food)
	}
	salary, ok := byCategory["Salary"]
	if !ok || salary["income"] != "2000.00" {
		t.Errorf("Salary row = %+v, want income 2000.00", salary)
	}
	uncategorized, ok := byCategory["Uncategorized"]
	if !ok || uncategorized["spending"] != "10.00" {
		t.Errorf("Uncategorized row = %+v, want spending 10.00", uncategorized)
	}
	if len(rows) != 3 {
		t.Errorf("rows = %+v, want exactly 3 (Food, Salary, Uncategorized) — a transfer must not appear", rows)
	}
}

// TestCategoryBreakdown_MissingCurrencyIs422 checks that an analytics
// query without a reporting currency surfaces the app layer's own
// InvalidInput as a 422, without this handler pre-validating the field
// itself (AnalyticsOptions.ReportingCurrency is required — see
// internal/app/analytics.go's validateAnalyticsOptions).
// TestCategoryBreakdown_MissingCurrencyFallsBackToReportingCurrency is
// issue #199's regression test: an analytics request with no ?currency=
// must resolve ADR-0004's ladder (the actor's configured reporting
// currency, then the instance default) via
// app.resolveAnalyticsOptions, the same way `bodger report` and `bodger
// balance` already fall back, rather than rejecting with a blank
// reporting_currency.
func TestCategoryBreakdown_MissingCurrencyFallsBackToReportingCurrency(t *testing.T) {
	srv := newTestServer(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC), "UTC")

	do(t, srv, http.MethodPost, "/api/v1/reporting-currency", map[string]any{"currency": "USD"})

	status, decoded := do(t, srv, http.MethodGet, "/api/v1/analytics/category-breakdown?policy=current", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %+v", status, decoded)
	}
	data := dataOf(t, decoded)
	if data["currency"] != "USD" {
		t.Errorf("currency = %v, want the configured reporting currency USD", data["currency"])
	}
}

// TestSavingsRate_ZeroIncomeOmitsRate checks that a period with outflow
// but no income renders "rate" as absent (not present, not zero) on the
// wire — an undefined ratio, per app.SavingsRateResult's own doc comment.
func TestSavingsRate_ZeroIncomeOmitsRate(t *testing.T) {
	frozenAt := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	srv := newTestServer(t, frozenAt, "UTC")

	do(t, srv, http.MethodPost, "/api/v1/accounts", map[string]any{"name": "Checking", "type": "bank", "currency": "USD"})
	do(t, srv, http.MethodPost, "/api/v1/transactions", map[string]any{
		"type": "outflow", "account": "Checking", "amount": "10", "description": "coffee",
	})

	status, decoded := do(t, srv, http.MethodGet, "/api/v1/analytics/savings-rate?currency=USD&policy=current", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200: %+v", status, decoded)
	}
	data := dataOf(t, decoded)
	if _, ok := data["rate"]; ok {
		t.Errorf("rate = %v, want absent (income is zero, an undefined ratio)", data["rate"])
	}
	if data["outflow"] != "10.00" {
		t.Errorf("outflow = %v, want 10.00", data["outflow"])
	}
}
