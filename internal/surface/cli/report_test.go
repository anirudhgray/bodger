package cli_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestReportCategoryBreakdown_GroupsByTopLevelCategoryAndExcludesTransfers
// is issue #188's CLI surface, exercised end to end - the CLI-side
// equivalent of internal/surface/http/analytics_test.go's
// TestCategoryBreakdown_GroupsByTopLevelCategoryAndExcludesTransfers
// (minus its uncategorized-bucket case: `spend`/`receive`'s own
// vocabulary always takes a category argument, so there's no CLI
// invocation shape that produces an uncategorized entry the way a bare
// POST /api/v1/transactions with no "category" field can - the HTTP test
// already covers that case).
func TestReportCategoryBreakdown_GroupsByTopLevelCategoryAndExcludesTransfers(t *testing.T) {
	factory := newTestFactory(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC))

	mustRun(t, factory, "accounts", "add", "Checking", "--type", "bank", "--currency", "USD")
	mustRun(t, factory, "accounts", "add", "Savings", "--type", "bank", "--currency", "USD")
	mustRun(t, factory, "categories", "add", "Food", "--type", "expense")
	mustRun(t, factory, "categories", "add", "Salary", "--type", "income")

	mustRun(t, factory, "spend", "50", "Food", "--account", "Checking")
	mustRun(t, factory, "receive", "2000", "Salary", "--account", "Checking")
	mustRun(t, factory, "move", "100", "--from", "Checking", "--to", "Savings")

	out := mustRun(t, factory, "report", "category-breakdown", "--currency", "USD", "--policy", "current", "--json")

	type row struct {
		Category string `json:"category"`
		Spending string `json:"spending"`
		Income   string `json:"income"`
	}
	var envelope struct {
		Data struct {
			Currency string `json:"currency"`
			Rows     []row  `json:"rows"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("decode --json output: %v (output: %s)", err, out)
	}

	byCategory := map[string]row{}
	for _, r := range envelope.Data.Rows {
		byCategory[r.Category] = r
	}
	if len(envelope.Data.Rows) != 2 {
		t.Fatalf("rows = %+v, want exactly 2 (Food, Salary) — a transfer must not appear", envelope.Data.Rows)
	}
	if byCategory["Food"].Spending != "50.00" {
		t.Errorf("Food row = %+v, want spending 50.00", byCategory["Food"])
	}
	if byCategory["Salary"].Income != "2000.00" {
		t.Errorf("Salary row = %+v, want income 2000.00", byCategory["Salary"])
	}
}

// TestReportSavingsRate_ZeroIncomeOmitsRate is
// internal/surface/http/analytics_test.go's
// TestSavingsRate_ZeroIncomeOmitsRate, on this CLI's own human-readable
// output: an undefined ratio renders as "n/a", never "0.0%".
func TestReportSavingsRate_ZeroIncomeOmitsRate(t *testing.T) {
	factory := newTestFactory(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC))
	mustRun(t, factory, "accounts", "add", "Checking", "--type", "bank", "--currency", "USD")
	mustRun(t, factory, "categories", "add", "Food", "--type", "expense")
	mustRun(t, factory, "spend", "10", "Food", "--account", "Checking")

	out := mustRun(t, factory, "report", "savings-rate", "--currency", "USD", "--policy", "current")
	if !strings.Contains(out, "savings rate n/a") {
		t.Errorf("output = %q, want it to report the savings rate as n/a (income is zero, an undefined ratio)", out)
	}
}

// TestReportCategoryBreakdown_MissingCurrencyIsRejected checks that
// omitting --currency surfaces the app layer's own InvalidInput, without
// this command pre-validating the flag itself.
func TestReportCategoryBreakdown_MissingCurrencyIsRejected(t *testing.T) {
	factory := newTestFactory(t, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC))
	_, _, err := run(t, factory, "report", "category-breakdown", "--policy", "current")
	if err == nil {
		t.Fatal("run: want an error when --currency is omitted, got nil")
	}
}
