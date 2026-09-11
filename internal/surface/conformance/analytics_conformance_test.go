// This file extends this package's conformance coverage to M5's four
// analytics queries (issue #188): GET /api/v1/analytics/* against
// `bodger report <metric>`. Like balance_conformance_test.go, this is a
// separate, parallel test function rather than another conformanceCase
// row: an analytics query is a GET with no body and its own
// list/aggregate-shaped response, which doesn't fit conformanceCase's
// "one command creates one comparable transaction record" shape - this
// reuses the harness (newHarness, seed, runCLI, runHTTP) but drives its
// own small table and comparison directly.
package conformance

import (
	"encoding/json"
	"testing"

	"github.com/anirudhgray/bodger/internal/app"
)

// analyticsUnconvertedView mirrors internal/surface/cli/report.go's and
// internal/surface/http/analytics.go's unconvertedPostingView field for
// field.
type analyticsUnconvertedView struct {
	TransactionID string `json:"transaction_id"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	Reason        string `json:"reason"`
}

type categoryBreakdownRowView struct {
	Category string `json:"category"`
	Spending string `json:"spending"`
	Income   string `json:"income"`
	Net      string `json:"net"`
}

type categoryBreakdownResultView struct {
	Currency    string                     `json:"currency"`
	Rows        []categoryBreakdownRowView `json:"rows"`
	Unconverted []analyticsUnconvertedView `json:"unconverted"`
}

type cashFlowPointView struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Inflow  string `json:"inflow"`
	Outflow string `json:"outflow"`
	Net     string `json:"net"`
}

type cashFlowResultView struct {
	Currency    string                     `json:"currency"`
	Points      []cashFlowPointView        `json:"points"`
	Unconverted []analyticsUnconvertedView `json:"unconverted"`
}

type trendPeriodView struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Inflow  string `json:"inflow"`
	Outflow string `json:"outflow"`
	Net     string `json:"net"`
}

type trendsResultView struct {
	Currency         string                     `json:"currency"`
	Current          trendPeriodView            `json:"current"`
	Previous         trendPeriodView            `json:"previous"`
	InflowChangePct  *float64                   `json:"inflow_change_pct,omitempty"`
	OutflowChangePct *float64                   `json:"outflow_change_pct,omitempty"`
	Unconverted      []analyticsUnconvertedView `json:"unconverted"`
}

type savingsRateResultView struct {
	Currency    string                     `json:"currency"`
	Income      string                     `json:"income"`
	Outflow     string                     `json:"outflow"`
	Net         string                     `json:"net"`
	Rate        *float64                   `json:"rate,omitempty"`
	Unconverted []analyticsUnconvertedView `json:"unconverted"`
}

// seedAnalyticsFixtures records a small set of transactions both this
// month (2026-08, hostileInstant's actor-timezone "today") and last month
// (2026-07) - enough for category-breakdown, cash-flow, trends, and
// savings-rate to each have something real to report on, and for trends
// specifically to see a genuine month-over-month change. It uses the
// application layer directly, the same way harness.seed and seedFxRate
// do - fixture setup, not part of what a case proves.
func (h *harness) seedAnalyticsFixtures() {
	h.t.Helper()
	ctx := h.t.Context()

	mustOutflow := func(account, category, amount, date string) {
		if _, err := h.svc.RecordOutflow(ctx, app.RecordOutflowCommand{
			ActorID: seededUserID, AccountRef: account, CategoryRef: category, Amount: amount, Date: date,
			Description: "conformance fixture",
		}); err != nil {
			h.t.Fatalf("seed outflow %s %s on %s: %v", amount, account, date, err)
		}
	}
	mustInflow := func(account, category, amount, date string) {
		if _, err := h.svc.RecordInflow(ctx, app.RecordInflowCommand{
			ActorID: seededUserID, AccountRef: account, CategoryRef: category, Amount: amount, Date: date,
			Description: "conformance fixture",
		}); err != nil {
			h.t.Fatalf("seed inflow %s %s on %s: %v", amount, account, date, err)
		}
	}

	// This month (2026-08).
	mustOutflow("HDFC Savings", "groceries", "500", "2026-08-01")
	mustInflow("HDFC Savings", "salary", "2000", "2026-08-01")
	mustOutflow("Cash", "groceries", "50", "2026-08-02")

	// Last month (2026-07), so Trends sees a real previous period.
	mustOutflow("HDFC Savings", "groceries", "300", "2026-07-15")
	mustInflow("HDFC Savings", "salary", "1800", "2026-07-15")
}

// TestAnalyticsConformance drives `bodger report <metric> --currency
// --policy --json` and `GET /api/v1/analytics/<metric>?currency=&policy=`
// against the same seeded fixtures, and asserts the two surfaces' results
// agree field for field - the same normalise-once contract TestConformance
// and TestBalanceQueryConformance enforce, applied here to M5's four
// analytics queries (issue #188).
func TestAnalyticsConformance(t *testing.T) {
	cases := []struct {
		name       string
		metric     string // report subcommand name and URL path segment
		extraArgs  []string
		extraQuery string
		decode     func(t *testing.T, cliOut string, httpData map[string]any) (cli, http any)
	}{
		{
			name:   "category-breakdown",
			metric: "category-breakdown",
			decode: func(t *testing.T, cliOut string, httpData map[string]any) (any, any) {
				return decodeCLIEnvelope[categoryBreakdownResultView](t, cliOut), remarshalInto[categoryBreakdownResultView](t, httpData)
			},
		},
		{
			name:       "cash-flow",
			metric:     "cash-flow",
			extraArgs:  []string{"--from", "2026-07-01", "--to", "2026-08-31"},
			extraQuery: "&from=2026-07-01&to=2026-08-31",
			decode: func(t *testing.T, cliOut string, httpData map[string]any) (any, any) {
				return decodeCLIEnvelope[cashFlowResultView](t, cliOut), remarshalInto[cashFlowResultView](t, httpData)
			},
		},
		{
			// Granularity coverage (issue #194): week and year bucketing,
			// plus the custom single-bucket mode, prove the CLI and REST
			// surfaces agree on the new field/param exactly the same way
			// the default-month case above already does.
			name:       "cash-flow-week",
			metric:     "cash-flow",
			extraArgs:  []string{"--from", "2026-07-01", "--to", "2026-08-31", "--granularity", "week"},
			extraQuery: "&from=2026-07-01&to=2026-08-31&granularity=week",
			decode: func(t *testing.T, cliOut string, httpData map[string]any) (any, any) {
				return decodeCLIEnvelope[cashFlowResultView](t, cliOut), remarshalInto[cashFlowResultView](t, httpData)
			},
		},
		{
			name:       "cash-flow-year",
			metric:     "cash-flow",
			extraArgs:  []string{"--from", "2026-07-01", "--to", "2026-08-31", "--granularity", "year"},
			extraQuery: "&from=2026-07-01&to=2026-08-31&granularity=year",
			decode: func(t *testing.T, cliOut string, httpData map[string]any) (any, any) {
				return decodeCLIEnvelope[cashFlowResultView](t, cliOut), remarshalInto[cashFlowResultView](t, httpData)
			},
		},
		{
			name:       "cash-flow-custom",
			metric:     "cash-flow",
			extraArgs:  []string{"--from", "2026-07-01", "--to", "2026-08-31", "--granularity", "custom"},
			extraQuery: "&from=2026-07-01&to=2026-08-31&granularity=custom",
			decode: func(t *testing.T, cliOut string, httpData map[string]any) (any, any) {
				return decodeCLIEnvelope[cashFlowResultView](t, cliOut), remarshalInto[cashFlowResultView](t, httpData)
			},
		},
		{
			name:   "trends",
			metric: "trends",
			decode: func(t *testing.T, cliOut string, httpData map[string]any) (any, any) {
				return decodeCLIEnvelope[trendsResultView](t, cliOut), remarshalInto[trendsResultView](t, httpData)
			},
		},
		{
			name:       "trends-week",
			metric:     "trends",
			extraArgs:  []string{"--granularity", "week"},
			extraQuery: "&granularity=week",
			decode: func(t *testing.T, cliOut string, httpData map[string]any) (any, any) {
				return decodeCLIEnvelope[trendsResultView](t, cliOut), remarshalInto[trendsResultView](t, httpData)
			},
		},
		{
			name:       "trends-year",
			metric:     "trends",
			extraArgs:  []string{"--granularity", "year"},
			extraQuery: "&granularity=year",
			decode: func(t *testing.T, cliOut string, httpData map[string]any) (any, any) {
				return decodeCLIEnvelope[trendsResultView](t, cliOut), remarshalInto[trendsResultView](t, httpData)
			},
		},
		{
			name:       "trends-custom",
			metric:     "trends",
			extraArgs:  []string{"--from", "2026-08-01", "--to", "2026-08-20", "--granularity", "custom"},
			extraQuery: "&from=2026-08-01&to=2026-08-20&granularity=custom",
			decode: func(t *testing.T, cliOut string, httpData map[string]any) (any, any) {
				return decodeCLIEnvelope[trendsResultView](t, cliOut), remarshalInto[trendsResultView](t, httpData)
			},
		},
		{
			name:       "savings-rate",
			metric:     "savings-rate",
			extraArgs:  []string{"--from", "2026-08-01", "--to", "2026-08-31"},
			extraQuery: "&from=2026-08-01&to=2026-08-31",
			decode: func(t *testing.T, cliOut string, httpData map[string]any) (any, any) {
				return decodeCLIEnvelope[savingsRateResultView](t, cliOut), remarshalInto[savingsRateResultView](t, httpData)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.seed()
			h.seedAnalyticsFixtures()

			args := append([]string{"report", tc.metric, "--currency", "INR", "--policy", "current"}, tc.extraArgs...)
			cliOut, cliErr := h.runCLI(args...)
			if cliErr != nil {
				t.Fatalf("CLI: unexpected error: %v (output: %s)", cliErr, cliOut)
			}

			path := "/api/v1/analytics/" + tc.metric + "?currency=INR&policy=current" + tc.extraQuery
			httpStatus, httpDecoded := h.runHTTP("GET", path, nil)
			if httpStatus >= 300 {
				t.Fatalf("HTTP: unexpected error status %d: %v", httpStatus, httpDecoded)
			}

			cliResult, httpResult := tc.decode(t, cliOut, h.httpData(httpDecoded))

			cliJSON, _ := json.MarshalIndent(cliResult, "", "  ")
			httpJSON, _ := json.MarshalIndent(httpResult, "", "  ")
			if string(cliJSON) != string(httpJSON) {
				t.Errorf("CLI and HTTP disagree on the %s result:\nCLI:\n%s\nHTTP:\n%s", tc.name, cliJSON, httpJSON)
			}
		})
	}
}

// decodeCLIEnvelope decodes runCLI's stdout - {"data": ...} on success,
// per internal/surface/cli's own --json envelope - into T.
func decodeCLIEnvelope[T any](t *testing.T, output string) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode CLI --json output: %v (output: %s)", err, output)
	}
	return envelope.Data
}

// remarshalInto re-marshals runHTTP's already-generic map[string]any data
// into T, so it's directly comparable to decodeCLIEnvelope's CLI-side
// result.
func remarshalInto[T any](t *testing.T, data map[string]any) T {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal HTTP data: %v", err)
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal HTTP data: %v (data: %s)", err, raw)
	}
	return v
}
