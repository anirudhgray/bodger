// This file extends this package's conformance coverage to issue #213's
// two download routes: GET /api/v1/export/json and GET /api/v1/export/csv
// against `bodger export json` and `bodger export csv`. Like
// balance_conformance_test.go and analytics_conformance_test.go, this is a
// separate, parallel test function rather than another conformanceCase
// row: a download's response body is a raw byte stream, not the usual
// {"data": ...} JSON envelope conformanceCase's own httpBody()/httpData()
// machinery (cases_test.go, harness_test.go) is built around — this
// reuses the harness (newHarness, seed, runCLI) but reads the HTTP
// response directly and compares raw bytes.
package conformance

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/anirudhgray/bodger/internal/app"
)

// runHTTPRaw sends method/path to h's server and returns its status code,
// Content-Type, Content-Disposition, and raw body bytes — unlike
// runHTTP, this never tries to decode the body as a {"data": ...}
// envelope, since a download route's body is the export document/CSV
// itself.
func (h *harness) runHTTPRaw(method, path string) (status int, contentType, contentDisposition string, body []byte) {
	h.t.Helper()

	req, err := http.NewRequest(method, h.srv.URL+path, nil)
	if err != nil {
		h.t.Fatalf("http.NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.token)

	resp, err := h.srv.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.t.Fatalf("%s %s: read response body: %v", method, path, err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Disposition"), respBody
}

// seedExportFixtures records a small set of transactions — a plain
// outflow and inflow on two different accounts, a transfer, and a second
// outflow with a tag — enough for the export conformance cases below to
// have something real to export and, for the CSV filter cases, more than
// one account/category actually worth distinguishing between.
func (h *harness) seedExportFixtures() {
	h.t.Helper()
	ctx := h.t.Context()

	if _, err := h.svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: seededUserID, AccountRef: "HDFC Savings", CategoryRef: "groceries", Amount: "500",
		Date: "2026-08-01", Description: "Big Bazaar", Tags: []string{"weekly"},
	}); err != nil {
		h.t.Fatalf("seed outflow: %v", err)
	}
	if _, err := h.svc.RecordInflow(ctx, app.RecordInflowCommand{
		ActorID: seededUserID, AccountRef: "HDFC Savings", CategoryRef: "salary", Amount: "50000",
		Date: "2026-08-01", Description: "Paycheck",
	}); err != nil {
		h.t.Fatalf("seed inflow: %v", err)
	}
	if _, err := h.svc.RecordOutflow(ctx, app.RecordOutflowCommand{
		ActorID: seededUserID, AccountRef: "Cash", CategoryRef: "groceries", Amount: "50",
		Date: "2026-08-02", Description: "Corner store",
	}); err != nil {
		h.t.Fatalf("seed cash outflow: %v", err)
	}
	if _, err := h.svc.RecordTransfer(ctx, app.RecordTransferCommand{
		ActorID: seededUserID, FromAccountRef: "HDFC Savings", ToAccountRef: "ICICI Checking",
		Amount: "1000", Date: "2026-08-03", Description: "Move to checking",
	}); err != nil {
		h.t.Fatalf("seed transfer: %v", err)
	}
}

// TestExportJSONConformance drives `bodger export json` and GET
// /api/v1/export/json against the same seeded fixtures and asserts the
// two surfaces return byte-identical documents — ADR-0005's normalise-once
// contract, applied to a download whose whole point (ADR-0008) is that
// identical underlying data always produces an identical export.
func TestExportJSONConformance(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.seedExportFixtures()

	cliOut, cliErr := h.runCLI("export", "json")
	if cliErr != nil {
		t.Fatalf("CLI: unexpected error: %v (output: %s)", cliErr, cliOut)
	}

	httpStatus, contentType, contentDisposition, httpBody := h.runHTTPRaw("GET", "/api/v1/export/json")
	if httpStatus != http.StatusOK {
		t.Fatalf("HTTP: unexpected status %d (body: %s)", httpStatus, httpBody)
	}
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Content-Type = %q, want it to contain %q", contentType, "application/json")
	}
	if !strings.Contains(contentDisposition, "attachment") {
		t.Errorf("Content-Disposition = %q, want it to contain %q", contentDisposition, "attachment")
	}

	if cliOut != string(httpBody) {
		t.Errorf("CLI and HTTP disagree on the JSON export document:\nCLI (%d bytes):\n%s\nHTTP (%d bytes):\n%s",
			len(cliOut), cliOut, len(httpBody), httpBody)
	}

	// Not a vacuous pass: confirm both sides actually exported real data,
	// not two empty (and therefore trivially equal) documents.
	var envelope struct {
		Format       string `json:"format"`
		Accounts     []any  `json:"accounts"`
		Categories   []any  `json:"categories"`
		Transactions []any  `json:"transactions"`
	}
	if err := json.Unmarshal(httpBody, &envelope); err != nil {
		t.Fatalf("HTTP body isn't valid JSON: %v (body: %s)", err, httpBody)
	}
	if envelope.Format != app.ExportFormatVersion {
		t.Errorf("format = %q, want %q", envelope.Format, app.ExportFormatVersion)
	}
	if len(envelope.Accounts) == 0 || len(envelope.Categories) == 0 || len(envelope.Transactions) == 0 {
		t.Fatalf("export is empty: accounts=%d categories=%d transactions=%d",
			len(envelope.Accounts), len(envelope.Categories), len(envelope.Transactions))
	}
}

// exportCSVConformanceCase is one row of TestExportCSVConformance's table:
// a filter, expressed once as CLI flags and once as query parameters,
// driven through both surfaces' real export-CSV entry points.
type exportCSVConformanceCase struct {
	name      string
	cliArgs   []string
	httpQuery url.Values
}

var exportCSVConformanceCases = []exportCSVConformanceCase{
	{
		name: "unfiltered",
	},
	{
		name:      "filtered by account",
		cliArgs:   []string{"--account", "HDFC Savings"},
		httpQuery: url.Values{"account": {"HDFC Savings"}},
	},
	{
		name:      "filtered by category",
		cliArgs:   []string{"--category", "groceries"},
		httpQuery: url.Values{"category": {"groceries"}},
	},
	{
		name:      "filtered by date range",
		cliArgs:   []string{"--from", "2026-08-02", "--to", "2026-08-03"},
		httpQuery: url.Values{"from": {"2026-08-02"}, "to": {"2026-08-03"}},
	},
	{
		name:      "filtered by tag",
		cliArgs:   []string{"--tag", "weekly"},
		httpQuery: url.Values{"tag": {"weekly"}},
	},
}

// TestExportCSVConformance drives `bodger export csv [filters]` and GET
// /api/v1/export/csv?[filters] against the same seeded fixtures and
// asserts the two surfaces return byte-identical CSV for the same filter
// — both unfiltered (a full export) and scoped by each of ADR-0009's
// filter dimensions the CSV export accepts (issue #213's own scope).
func TestExportCSVConformance(t *testing.T) {
	for _, tc := range exportCSVConformanceCases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.seed()
			h.seedExportFixtures()

			cliOut, cliErr := h.runCLI(append([]string{"export", "csv"}, tc.cliArgs...)...)
			if cliErr != nil {
				t.Fatalf("CLI: unexpected error: %v (output: %s)", cliErr, cliOut)
			}

			path := "/api/v1/export/csv"
			if len(tc.httpQuery) > 0 {
				path += "?" + tc.httpQuery.Encode()
			}
			httpStatus, contentType, contentDisposition, httpBody := h.runHTTPRaw("GET", path)
			if httpStatus != http.StatusOK {
				t.Fatalf("HTTP: unexpected status %d (body: %s)", httpStatus, httpBody)
			}
			if !strings.Contains(contentType, "text/csv") {
				t.Errorf("Content-Type = %q, want it to contain %q", contentType, "text/csv")
			}
			if !strings.Contains(contentDisposition, "attachment") {
				t.Errorf("Content-Disposition = %q, want it to contain %q", contentDisposition, "attachment")
			}

			if cliOut != string(httpBody) {
				t.Errorf("CLI and HTTP disagree on the CSV export:\nCLI (%d bytes):\n%s\nHTTP (%d bytes):\n%s",
					len(cliOut), cliOut, len(httpBody), httpBody)
			}

			rows := strings.Split(strings.TrimRight(cliOut, "\n"), "\n")
			if len(rows) < 2 {
				t.Fatalf("CSV export has no data rows at all (only %d line(s)) — the comparison above would pass vacuously", len(rows))
			}
		})
	}
}
