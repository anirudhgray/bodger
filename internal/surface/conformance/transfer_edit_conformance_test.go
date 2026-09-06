// This file extends this package's conformance coverage to issue #163:
// a cross-currency transfer's from-leg and to-leg amounts must both
// survive a GET-then-PATCH round trip unchanged, on both surfaces. It's a
// separate, sequential test function rather than another conformanceCase
// row (cases_test.go): that table and its dispatch machinery are
// purpose-built around "one command creates one comparable transaction
// record" — a single POST, compared field by field — not a create,
// read-it-back, then edit-with-the-values-just-read sequence.
//
// The HTTP side drives a literal GET then PATCH. The CLI has no
// single-transaction "get" — `transactions list` is its own closest
// analogue, and the one `transactions edit`'s own doc comment already
// points a caller at ("run `bodger transactions list` first to see
// what's there now") — so the CLI side lists, finds the transfer by ID,
// and re-submits `transactions edit` with exactly the amount/to_amount
// values that listing just reported, the same way TestTransactions_Edit-
// CrossCurrencyMoveRoundTripPreservesBothLegs (internal/surface/cli/
// cli_test.go) already checks for the CLI alone. What this test adds is
// asserting the two surfaces agree — not just that each one's round trip
// happens not to corrupt itself.
package conformance

import "testing"

// TestCrossCurrencyTransferEditRoundTrip_HTTP is issue #163's core "done
// when": internal/surface/http/dto.go's transactionView reports a
// transfer's amount/currency as the from-leg and to_amount/to_currency as
// the to-leg (its own doc comment explains why those specific names and
// that specific direction) — so a client that GETs a transfer and PATCHes
// it back with those exact fields, unchanged, must get the identical
// transfer back, not one with a leg silently swapped or scaled. Before
// the fix, transactionView reported only the to-leg (as "amount", with no
// to_amount field at all), so this same round trip — POST amount=100
// (USD) => GET reports amount=8000 (INR, the to-leg) => PATCH amount=8000
// (reinterpreted as a *new* from-leg, in the from-account's own USD) —
// silently turned a 100 USD -> 8000 INR transfer into an 8000 USD ->
// 8000 INR one.
func TestCrossCurrencyTransferEditRoundTrip_HTTP(t *testing.T) {
	h := newHarness(t)
	h.seed() // Cash (USD), HDFC Savings (INR), among others.

	createBody := map[string]any{
		"from_account": "Cash", "to_account": "HDFC Savings",
		"amount": "100", "to_amount": "8000",
		"date": "2026-01-15", "description": "USD to INR",
	}
	status, decoded := h.runHTTP("POST", "/api/v1/transfers", createBody)
	if status != 201 {
		t.Fatalf("create transfer: status = %d, want 201: %+v", status, decoded)
	}
	created := h.httpData(decoded)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created transfer has no id: %+v", created)
	}
	if created["amount"] != "100.00" || created["currency"] != "USD" {
		t.Fatalf("created from-leg = %v %v, want 100.00 USD", created["amount"], created["currency"])
	}
	if created["to_amount"] != "8000.00" || created["to_currency"] != "INR" {
		t.Fatalf("created to-leg = %v %v, want 8000.00 INR", created["to_amount"], created["to_currency"])
	}

	status, decoded = h.runHTTP("GET", "/api/v1/transactions/"+id, nil)
	if status != 200 {
		t.Fatalf("get transfer: status = %d, want 200: %+v", status, decoded)
	}
	got := h.httpData(decoded)

	// Resubmit exactly what GET just reported — the shape a client that
	// pre-fills an edit form from a GET response actually sends, with
	// nothing about the transfer changed.
	editBody := map[string]any{
		"from_account": got["from_account_id"], "to_account": got["to_account_id"],
		"amount": got["amount"], "to_amount": got["to_amount"],
		"date": got["date"], "description": got["description"],
	}
	status, decoded = h.runHTTP("PATCH", "/api/v1/transactions/"+id, editBody)
	if status != 200 {
		t.Fatalf("edit transfer: status = %d, want 200: %+v", status, decoded)
	}
	edited := h.httpData(decoded)

	if edited["amount"] != "100.00" || edited["currency"] != "USD" {
		t.Errorf("edited from-leg = %v %v, want 100.00 USD (from-leg corrupted by the round trip)", edited["amount"], edited["currency"])
	}
	if edited["to_amount"] != "8000.00" || edited["to_currency"] != "INR" {
		t.Errorf("edited to-leg = %v %v, want 8000.00 INR (to-leg corrupted by the round trip)", edited["to_amount"], edited["to_currency"])
	}

	// A second GET confirms the corrected values are what's actually
	// stored, not just what the PATCH response happened to echo back.
	status, decoded = h.runHTTP("GET", "/api/v1/transactions/"+id, nil)
	if status != 200 {
		t.Fatalf("re-get transfer: status = %d, want 200: %+v", status, decoded)
	}
	final := h.httpData(decoded)
	if final["amount"] != "100.00" || final["currency"] != "USD" || final["to_amount"] != "8000.00" || final["to_currency"] != "INR" {
		t.Errorf("stored transfer after round trip = %+v, want 100.00 USD -> 8000.00 INR", final)
	}
}

// TestCrossCurrencyTransferEditRoundTrip_CLI is the CLI-surface half of
// the same round trip, using `transactions list` as the CLI's own
// closest read-then-edit analogue (see this file's own doc comment for
// why there's no literal CLI "get").
func TestCrossCurrencyTransferEditRoundTrip_CLI(t *testing.T) {
	h := newHarness(t)
	h.seed()

	out, err := h.runCLI("move", "100", "--from", "Cash", "--to", "HDFC Savings", "--to-amount", "8000", "--on", "2026-01-15")
	if err != nil {
		t.Fatalf("move: unexpected error: %v (output: %s)", err, out)
	}
	created := decodeEnvelope(t, out)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created move has no id: %+v", created)
	}

	out, err = h.runCLI("transactions", "list")
	if err != nil {
		t.Fatalf("transactions list: unexpected error: %v (output: %s)", err, out)
	}
	listed := decodeEnvelope(t, out)
	rows, _ := listed["transactions"].([]any)
	var got map[string]any
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if ok && m["id"] == id {
			got = m
			break
		}
	}
	if got == nil {
		t.Fatalf("transactions list didn't include %s: %+v", id, rows)
	}
	if got["amount"] != "100.00" || got["currency"] != "USD" {
		t.Fatalf("listed from-leg = %v %v, want 100.00 USD", got["amount"], got["currency"])
	}
	if got["to_amount"] != "8000.00" || got["to_currency"] != "INR" {
		t.Fatalf("listed to-leg = %v %v, want 8000.00 INR", got["to_amount"], got["to_currency"])
	}

	fromAccountID, _ := got["from_account_id"].(string)
	toAccountID, _ := got["to_account_id"].(string)
	amount, _ := got["amount"].(string)
	toAmount, _ := got["to_amount"].(string)
	description, _ := got["description"].(string)
	date, _ := got["date"].(string)

	// Resubmit exactly what `transactions list` just reported.
	out, err = h.runCLI("transactions", "edit", id,
		"--amount", amount, "--from", fromAccountID, "--to", toAccountID, "--to-amount", toAmount,
		"--description", description, "--on", date)
	if err != nil {
		t.Fatalf("transactions edit: unexpected error: %v (output: %s)", err, out)
	}
	edited := decodeEnvelope(t, out)

	if edited["amount"] != "100.00" || edited["currency"] != "USD" {
		t.Errorf("edited from-leg = %v %v, want 100.00 USD (from-leg corrupted by the round trip)", edited["amount"], edited["currency"])
	}
	if edited["to_amount"] != "8000.00" || edited["to_currency"] != "INR" {
		t.Errorf("edited to-leg = %v %v, want 8000.00 INR (to-leg corrupted by the round trip)", edited["to_amount"], edited["to_currency"])
	}
}
