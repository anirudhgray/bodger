// This file extends this package's conformance coverage (issue #137) to
// GET /api/v1/balances with a target currency and policy against
// `bodger balance --currency --policy`. It's a separate, parallel test
// function rather than another conformanceCase row: conformanceCase and
// its cliArgs()/httpMethod()/httpPath()/httpBody() methods (cases_test.go)
// are purpose-built around "one command creates one comparable
// transaction record" — a single POST with a body, compared field by
// field. A balance query is a GET with no body and a list-shaped
// response (balances[] plus unconverted[]), which doesn't fit that
// shape, so this reuses the harness (newHarness, seed, runCLI, runHTTP)
// but drives its own small table and comparison directly, rather than
// forcing it through conformanceCase/dispatch-style machinery built for a
// different job.
package conformance

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/ports"
)

// balanceConvertedView is the "converted" shape both
// internal/surface/cli/balance.go's convertedBalanceView and
// internal/surface/http/dto.go's convertedBalanceView render — field for
// field, per issue #137.
type balanceConvertedView struct {
	Amount     string `json:"amount"`
	Currency   string `json:"currency"`
	Rate       string `json:"rate"`
	RateDate   string `json:"rate_date"`
	RateSource string `json:"rate_source"`
	Stale      bool   `json:"stale"`
	Policy     string `json:"policy"`
}

// balanceEntryView deliberately omits the raw (unconverted) balance
// amount: internal/surface/cli's balanceEntryView calls that field
// "balance" while internal/surface/http's balanceView calls it "amount" —
// an existing, pre-#137 naming asymmetry between the two surfaces (like
// cases_test.go's "account"/"category" label fields), not something this
// test is checking. Account/Currency/Converted are what issue #137 added
// and what this test exists to compare.
type balanceEntryView struct {
	Account   string                `json:"account"`
	Currency  string                `json:"currency"`
	Converted *balanceConvertedView `json:"converted"`
}

type unconvertedEntryView struct {
	Account string `json:"account"`
	Reason  string `json:"reason"`
}

type balancesResultView struct {
	Balances    []balanceEntryView     `json:"balances"`
	Unconverted []unconvertedEntryView `json:"unconverted"`
}

// balanceQueryCase is one row of TestBalanceQueryConformance's table: a
// target currency and policy, driven through both surfaces' real balance
// query entry points against the same seeded accounts and stored rate.
type balanceQueryCase struct {
	name     string
	currency string
	policy   string
}

var balanceQueryCases = []balanceQueryCase{
	{name: "current policy", currency: "USD", policy: "current"},
	{name: "transaction_date policy", currency: "USD", policy: "transaction_date"},
}

// TestBalanceQueryConformance drives `bodger balance --currency --policy
// --json` and `GET /api/v1/balances?currency=&policy=` against the same
// seeded accounts (harness.seed: HDFC Savings/INR, ICICI Checking/INR,
// Cash/USD, Wallet/USD, WALLET/USD) and one stored INR/USD rate dated
// "today" (2026-08-01, the actor-timezone date hostileInstant resolves
// to — so both PolicyCurrent and PolicyTransactionDate resolve the same
// underlying rate here), and asserts the two surfaces'
// balances[].converted/unconverted shapes agree field for field — the
// same normalise-once contract TestConformance enforces for a single
// transaction, applied here to a list-shaped read.
func TestBalanceQueryConformance(t *testing.T) {
	for _, tc := range balanceQueryCases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.seed()
			h.seedFxRate("INR", "USD", "0.0115", mustHarnessDate(t, 2026, time.August, 1), "conformance-seed")

			cliOut, cliErr := h.runCLI("balance", "--currency", tc.currency, "--policy", tc.policy)
			if cliErr != nil {
				t.Fatalf("CLI: unexpected error: %v (output: %s)", cliErr, cliOut)
			}
			httpStatus, httpDecoded := h.runHTTP("GET", "/api/v1/balances?currency="+tc.currency+"&policy="+tc.policy, nil)
			if httpStatus >= 300 {
				t.Fatalf("HTTP: unexpected error status %d: %v", httpStatus, httpDecoded)
			}

			cliResult := decodeBalancesEnvelope(t, cliOut)
			httpResult := balancesResultFromHTTP(t, h.httpData(httpDecoded))

			cliJSON, _ := json.MarshalIndent(normalizeBalances(cliResult), "", "  ")
			httpJSON, _ := json.MarshalIndent(normalizeBalances(httpResult), "", "  ")
			if string(cliJSON) != string(httpJSON) {
				t.Errorf("CLI and HTTP disagree on the balance query result:\nCLI:\n%s\nHTTP:\n%s", cliJSON, httpJSON)
			}
			if len(cliResult.Balances) == 0 {
				t.Fatalf("no balances returned at all — the comparison above would pass vacuously")
			}
		})
	}
}

// seedFxRate stores one rate directly through h's shared Service's
// FxRates repository — fixture setup via the application layer, the same
// way harness.seed creates accounts and categories directly rather than
// through either surface (harness_test.go's doc comment). This bypasses
// FetchFxRates/FxProvider entirely, so it never depends on the network the
// harness's real fxprovider.Frankfurter would otherwise need.
func (h *harness) seedFxRate(base, quote, value string, date domain.Date, source string) {
	h.t.Helper()
	rate, err := fx.NewRate(base, quote, decimal.RequireFromString(value))
	if err != nil {
		h.t.Fatalf("fx.NewRate(%s, %s, %s): %v", base, quote, value, err)
	}
	if err := h.svc.FxRates.StoreBatch(context.Background(), []ports.FxRateRow{{Rate: rate, Date: date, Source: source}}); err != nil {
		h.t.Fatalf("seedFxRate: StoreBatch: %v", err)
	}
}

// mustHarnessDate constructs a domain.Date, failing the test outright on
// an invalid one — this package's own fixture dates are always valid by
// construction, so a failure here means a typo in the test itself.
func mustHarnessDate(t *testing.T, year int, month time.Month, day int) domain.Date {
	t.Helper()
	d, err := domain.NewDate(year, month, day)
	if err != nil {
		t.Fatalf("domain.NewDate(%d, %s, %d): %v", year, month, day, err)
	}
	return d
}

// decodeBalancesEnvelope decodes runCLI's stdout — {"data": {"balances":
// ..., "unconverted": ...}} on success, per internal/surface/cli's own
// --json envelope.
func decodeBalancesEnvelope(t *testing.T, output string) balancesResultView {
	t.Helper()
	var envelope struct {
		Data balancesResultView `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode CLI --json output: %v (output: %s)", err, output)
	}
	return envelope.Data
}

// balancesResultFromHTTP re-marshals decoded's already-generic
// map[string]any shape into balancesResultView, so it's directly
// comparable to decodeBalancesEnvelope's CLI-side result.
func balancesResultFromHTTP(t *testing.T, data map[string]any) balancesResultView {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal HTTP data: %v", err)
	}
	var v balancesResultView
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal HTTP data into balancesResultView: %v (data: %s)", err, raw)
	}
	return v
}

// normalizeBalances keys each entry by its account name rather than list
// order, so a harmless difference in enumeration order between the two
// surfaces never causes a false mismatch.
func normalizeBalances(v balancesResultView) map[string]any {
	balances := map[string]balanceEntryView{}
	for _, b := range v.Balances {
		balances[b.Account] = b
	}
	unconverted := map[string]unconvertedEntryView{}
	for _, u := range v.Unconverted {
		unconverted[u.Account] = u
	}
	return map[string]any{"balances": balances, "unconverted": unconverted}
}
