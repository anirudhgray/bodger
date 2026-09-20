package importing_test

import (
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

func mustRecordMoney(t *testing.T, amountMinor int64, currency string) money.Money {
	t.Helper()
	m, err := money.NewMoney(amountMinor, currency)
	if err != nil {
		t.Fatalf("NewMoney(%d, %q) = %v, want success", amountMinor, currency, err)
	}
	return m
}

func mustRecordDate(t *testing.T, y int, mo int, d int) domain.Date {
	t.Helper()
	date, err := domain.NewDate(y, time.Month(mo), d)
	if err != nil {
		t.Fatalf("NewDate(%d, %d, %d) = %v, want success", y, mo, d, err)
	}
	return date
}

func mustRecord(t *testing.T) importing.ImportRecord {
	t.Helper()
	r, err := importing.NewImportRecord(
		"record-1", "user-1", "batch-1", `{"date":"2026-08-14","amount":"-12.34"}`,
		mustRecordDate(t, 2026, 8, 14), "Coffee shop", mustRecordMoney(t, -1234, "USD"), 0,
	)
	if err != nil {
		t.Fatalf("NewImportRecord(...) = %v, want success", err)
	}
	return r
}

func TestNewImportRecord(t *testing.T) {
	t.Parallel()

	t.Run("starts pending", func(t *testing.T) {
		t.Parallel()
		r := mustRecord(t)
		if r.Status() != importing.ImportRecordStatusPending {
			t.Errorf("Status() = %q, want %q", r.Status(), importing.ImportRecordStatusPending)
		}
	})

	t.Run("round-trips its required fields", func(t *testing.T) {
		t.Parallel()
		date := mustRecordDate(t, 2026, 8, 14)
		amount := mustRecordMoney(t, -1234, "USD")
		r, err := importing.NewImportRecord("record-1", "user-1", "batch-1", "raw-row", date, "Coffee shop", amount, 3)
		if err != nil {
			t.Fatalf("NewImportRecord(...) = %v, want success", err)
		}
		if r.ID() != "record-1" {
			t.Errorf("ID() = %q, want %q", r.ID(), "record-1")
		}
		if r.UserID() != "user-1" {
			t.Errorf("UserID() = %q, want %q", r.UserID(), "user-1")
		}
		if r.ImportBatchID() != "batch-1" {
			t.Errorf("ImportBatchID() = %q, want %q", r.ImportBatchID(), "batch-1")
		}
		if r.RawPayload() != "raw-row" {
			t.Errorf("RawPayload() = %q, want %q", r.RawPayload(), "raw-row")
		}
		if !r.BookedDate().Equal(date) {
			t.Errorf("BookedDate() = %s, want %s", r.BookedDate(), date)
		}
		if r.Description() != "Coffee shop" {
			t.Errorf("Description() = %q, want %q", r.Description(), "Coffee shop")
		}
		if !r.Amount().Equal(amount) {
			t.Errorf("Amount() = %s, want %s", r.Amount(), amount)
		}
		if r.SortOrder() != 3 {
			t.Errorf("SortOrder() = %d, want 3", r.SortOrder())
		}
		if _, ok := r.ExternalID(); ok {
			t.Error("ExternalID() ok = true, want false when never set")
		}
		if _, ok := r.PostedDate(); ok {
			t.Error("PostedDate() ok = true, want false when never set")
		}
		if _, ok := r.ResolvedAccountID(); ok {
			t.Error("ResolvedAccountID() ok = true, want false when never set")
		}
		if _, ok := r.ResolvedCategoryID(); ok {
			t.Error("ResolvedCategoryID() ok = true, want false when never set")
		}
		if _, ok := r.DuplicateMatch(); ok {
			t.Error("DuplicateMatch() ok = true, want false when never set")
		}
		if _, ok := r.TransactionID(); ok {
			t.Error("TransactionID() ok = true, want false when never committed")
		}
	})

	for _, tt := range []struct {
		name          string
		id            string
		userID        string
		importBatchID string
		rawPayload    string
		description   string
		wantErr       error
	}{
		{"empty id", "", "user-1", "batch-1", "raw", "desc", importing.ErrImportRecordEmptyID},
		{"empty user id", "record-1", "", "batch-1", "raw", "desc", importing.ErrImportRecordEmptyUserID},
		{"empty import batch id", "record-1", "user-1", "", "raw", "desc", importing.ErrImportRecordEmptyImportBatchID},
		{"empty raw payload", "record-1", "user-1", "batch-1", "", "desc", importing.ErrImportRecordEmptyRawPayload},
		{"empty description", "record-1", "user-1", "batch-1", "raw", "", importing.ErrImportRecordEmptyDescription},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := importing.NewImportRecord(tt.id, tt.userID, tt.importBatchID, tt.rawPayload,
				mustRecordDate(t, 2026, 8, 14), tt.description, mustRecordMoney(t, -100, "USD"), 0)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewImportRecord(...) error = %v, want %v", err, tt.wantErr)
			}
		})
	}

	t.Run("options set optional fields", func(t *testing.T) {
		t.Parallel()
		postedDate := mustRecordDate(t, 2026, 8, 16)
		match, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		r, err := importing.NewImportRecord(
			"record-1", "user-1", "batch-1", "raw", mustRecordDate(t, 2026, 8, 14), "desc",
			mustRecordMoney(t, -100, "USD"), 0,
			importing.WithPostedDate(postedDate),
			importing.WithExternalID("ext-1"),
			importing.WithResolvedAccount("acc-1"),
			importing.WithResolvedCategory("cat-1"),
			importing.WithDuplicateMatch(match),
			importing.WithTransferCandidate("record-2"),
		)
		if err != nil {
			t.Fatalf("NewImportRecord(...) = %v, want success", err)
		}
		if d, ok := r.PostedDate(); !ok || !d.Equal(postedDate) {
			t.Errorf("PostedDate() = (%s, %v), want (%s, true)", d, ok, postedDate)
		}
		if v, ok := r.ExternalID(); !ok || v != "ext-1" {
			t.Errorf("ExternalID() = (%q, %v), want (%q, true)", v, ok, "ext-1")
		}
		if v, ok := r.ResolvedAccountID(); !ok || v != "acc-1" {
			t.Errorf("ResolvedAccountID() = (%q, %v), want (%q, true)", v, ok, "acc-1")
		}
		if v, ok := r.ResolvedCategoryID(); !ok || v != "cat-1" {
			t.Errorf("ResolvedCategoryID() = (%q, %v), want (%q, true)", v, ok, "cat-1")
		}
		if dm, ok := r.DuplicateMatch(); !ok || dm.MatchedTransactionID() != "txn-1" {
			t.Errorf("DuplicateMatch() = (%+v, %v), want a match against txn-1", dm, ok)
		}
		if v, ok := r.TransferCandidateRecordID(); !ok || v != "record-2" {
			t.Errorf("TransferCandidateRecordID() = (%q, %v), want (%q, true)", v, ok, "record-2")
		}
	})

	t.Run("WithTransferCandidate ignores an empty record id", func(t *testing.T) {
		t.Parallel()
		r, err := importing.NewImportRecord(
			"record-1", "user-1", "batch-1", "raw", mustRecordDate(t, 2026, 8, 14), "desc",
			mustRecordMoney(t, -100, "USD"), 0,
			importing.WithTransferCandidate(""),
		)
		if err != nil {
			t.Fatalf("NewImportRecord(...) = %v, want success", err)
		}
		if _, ok := r.TransferCandidateRecordID(); ok {
			t.Error("TransferCandidateRecordID() ok = true, want false for an empty option value")
		}
	})

	t.Run("WithRecordStatus and WithTransactionID reconstruct a committed record", func(t *testing.T) {
		t.Parallel()
		r, err := importing.NewImportRecord(
			"record-1", "user-1", "batch-1", "raw", mustRecordDate(t, 2026, 8, 14), "desc",
			mustRecordMoney(t, -100, "USD"), 0,
			importing.WithRecordStatus(importing.ImportRecordStatusCommitted),
			importing.WithTransactionID("txn-9"),
		)
		if err != nil {
			t.Fatalf("NewImportRecord(...) = %v, want success", err)
		}
		if r.Status() != importing.ImportRecordStatusCommitted {
			t.Errorf("Status() = %q, want %q", r.Status(), importing.ImportRecordStatusCommitted)
		}
		if v, ok := r.TransactionID(); !ok || v != "txn-9" {
			t.Errorf("TransactionID() = (%q, %v), want (%q, true)", v, ok, "txn-9")
		}
	})

	t.Run("rejects an unknown status", func(t *testing.T) {
		t.Parallel()
		_, err := importing.NewImportRecord(
			"record-1", "user-1", "batch-1", "raw", mustRecordDate(t, 2026, 8, 14), "desc",
			mustRecordMoney(t, -100, "USD"), 0,
			importing.WithRecordStatus(importing.ImportRecordStatus("bogus")),
		)
		if !errors.Is(err, importing.ErrImportRecordInvalidStatus) {
			t.Fatalf("NewImportRecord(...) error = %v, want ErrImportRecordInvalidStatus", err)
		}
	})

	t.Run("rejects committed status with no transaction id", func(t *testing.T) {
		t.Parallel()
		_, err := importing.NewImportRecord(
			"record-1", "user-1", "batch-1", "raw", mustRecordDate(t, 2026, 8, 14), "desc",
			mustRecordMoney(t, -100, "USD"), 0,
			importing.WithRecordStatus(importing.ImportRecordStatusCommitted),
		)
		if !errors.Is(err, importing.ErrImportRecordEmptyTransactionID) {
			t.Fatalf("NewImportRecord(...) error = %v, want ErrImportRecordEmptyTransactionID", err)
		}
	})
}

// TestImportRecord_StatusStateMachine exercises both branches of
// ADR-0008's per-row pipeline — pending -> ready -> committed, and
// pending -> excluded — and confirms every other transition attempt is
// rejected.
func TestImportRecord_StatusStateMachine(t *testing.T) {
	t.Parallel()

	t.Run("the happy path runs pending -> ready -> committed", func(t *testing.T) {
		t.Parallel()
		r := mustRecord(t)

		ready, err := r.MarkReady()
		if err != nil {
			t.Fatalf("MarkReady() = %v, want success", err)
		}
		if ready.Status() != importing.ImportRecordStatusReady {
			t.Errorf("Status() = %q, want %q", ready.Status(), importing.ImportRecordStatusReady)
		}
		// r itself must be unchanged (copy-transform, not mutation).
		if r.Status() != importing.ImportRecordStatusPending {
			t.Errorf("original record Status() = %q after MarkReady, want unchanged %q", r.Status(), importing.ImportRecordStatusPending)
		}

		committed, err := ready.MarkCommitted("txn-1")
		if err != nil {
			t.Fatalf("MarkCommitted(...) = %v, want success", err)
		}
		if committed.Status() != importing.ImportRecordStatusCommitted {
			t.Errorf("Status() = %q, want %q", committed.Status(), importing.ImportRecordStatusCommitted)
		}
		if v, ok := committed.TransactionID(); !ok || v != "txn-1" {
			t.Errorf("TransactionID() = (%q, %v), want (%q, true)", v, ok, "txn-1")
		}
	})

	t.Run("the excluded branch runs pending -> excluded", func(t *testing.T) {
		t.Parallel()
		r := mustRecord(t)

		excluded, err := r.MarkExcluded()
		if err != nil {
			t.Fatalf("MarkExcluded() = %v, want success", err)
		}
		if excluded.Status() != importing.ImportRecordStatusExcluded {
			t.Errorf("Status() = %q, want %q", excluded.Status(), importing.ImportRecordStatusExcluded)
		}
	})

	t.Run("MarkCommitted rejects an empty transaction id", func(t *testing.T) {
		t.Parallel()
		r, err := mustRecord(t).MarkReady()
		if err != nil {
			t.Fatalf("MarkReady() = %v, want success", err)
		}
		if _, err := r.MarkCommitted(""); !errors.Is(err, importing.ErrImportRecordEmptyTransactionID) {
			t.Fatalf("MarkCommitted(\"\") error = %v, want ErrImportRecordEmptyTransactionID", err)
		}
	})

	for _, tt := range []struct {
		name  string
		from  importing.ImportRecordStatus
		apply func(importing.ImportRecord) (importing.ImportRecord, error)
	}{
		{"MarkReady from ready", importing.ImportRecordStatusReady, importing.ImportRecord.MarkReady},
		{"MarkReady from excluded", importing.ImportRecordStatusExcluded, importing.ImportRecord.MarkReady},
		{"MarkReady from committed", importing.ImportRecordStatusCommitted, importing.ImportRecord.MarkReady},
		{"MarkExcluded from ready", importing.ImportRecordStatusReady, importing.ImportRecord.MarkExcluded},
		{"MarkExcluded from excluded", importing.ImportRecordStatusExcluded, importing.ImportRecord.MarkExcluded},
		{"MarkExcluded from committed", importing.ImportRecordStatusCommitted, importing.ImportRecord.MarkExcluded},
	} {
		tt := tt
		t.Run("rejects "+tt.name, func(t *testing.T) {
			t.Parallel()
			r, err := importing.NewImportRecord(
				"record-1", "user-1", "batch-1", "raw", mustRecordDate(t, 2026, 8, 14), "desc",
				mustRecordMoney(t, -100, "USD"), 0,
				importing.WithRecordStatus(tt.from),
				importing.WithTransactionID("txn-1"),
			)
			if err != nil {
				t.Fatalf("NewImportRecord(...) = %v, want success", err)
			}
			if _, err := tt.apply(r); !errors.Is(err, importing.ErrImportRecordInvalidTransition) {
				t.Fatalf("transition error = %v, want ErrImportRecordInvalidTransition", err)
			}
		})
	}

	t.Run("rejects MarkCommitted from pending", func(t *testing.T) {
		t.Parallel()
		r := mustRecord(t)
		if _, err := r.MarkCommitted("txn-1"); !errors.Is(err, importing.ErrImportRecordInvalidTransition) {
			t.Fatalf("MarkCommitted(...) error = %v, want ErrImportRecordInvalidTransition", err)
		}
	})

	t.Run("rejects MarkCommitted from excluded", func(t *testing.T) {
		t.Parallel()
		r, err := mustRecord(t).MarkExcluded()
		if err != nil {
			t.Fatalf("MarkExcluded() = %v, want success", err)
		}
		if _, err := r.MarkCommitted("txn-1"); !errors.Is(err, importing.ErrImportRecordInvalidTransition) {
			t.Fatalf("MarkCommitted(...) error = %v, want ErrImportRecordInvalidTransition", err)
		}
	})
}

// TestImportRecord_Resolve covers ADR-0008's "the decision is recorded on
// the ImportRecord for auditability" — Resolve delegates to the record's
// own DuplicateMatch and stores the result back on the record.
func TestImportRecord_Resolve(t *testing.T) {
	t.Parallel()

	t.Run("resolves the record's duplicate match", func(t *testing.T) {
		t.Parallel()
		match, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		r, err := importing.NewImportRecord(
			"record-1", "user-1", "batch-1", "raw", mustRecordDate(t, 2026, 8, 14), "desc",
			mustRecordMoney(t, -100, "USD"), 0,
			importing.WithDuplicateMatch(match),
		)
		if err != nil {
			t.Fatalf("NewImportRecord(...) = %v, want success", err)
		}

		resolved, err := r.Resolve(importing.DuplicateResolutionDismissed)
		if err != nil {
			t.Fatalf("Resolve(dismissed) = %v, want success", err)
		}
		dm, ok := resolved.DuplicateMatch()
		if !ok {
			t.Fatal("DuplicateMatch() ok = false after Resolve, want true")
		}
		if dm.Resolution() != importing.DuplicateResolutionDismissed {
			t.Errorf("Resolution() = %q, want %q", dm.Resolution(), importing.DuplicateResolutionDismissed)
		}
	})

	t.Run("rejects resolving a record with no duplicate match", func(t *testing.T) {
		t.Parallel()
		r := mustRecord(t)
		if _, err := r.Resolve(importing.DuplicateResolutionDismissed); !errors.Is(err, importing.ErrImportRecordNoDuplicateMatch) {
			t.Fatalf("Resolve(...) error = %v, want ErrImportRecordNoDuplicateMatch", err)
		}
	})
}

// TestImportRecord_ResolveOccurrenceMatch is TestImportRecord_Resolve's
// counterpart for issue #309's OccurrenceMatch.
func TestImportRecord_ResolveOccurrenceMatch(t *testing.T) {
	t.Parallel()

	t.Run("resolves the record's occurrence match", func(t *testing.T) {
		t.Parallel()
		match, err := importing.NewOccurrenceMatch("occ-1")
		if err != nil {
			t.Fatalf("NewOccurrenceMatch(...) = %v, want success", err)
		}
		r, err := importing.NewImportRecord(
			"record-1", "user-1", "batch-1", "raw", mustRecordDate(t, 2026, 8, 14), "desc",
			mustRecordMoney(t, -100, "USD"), 0,
			importing.WithOccurrenceMatch(match),
		)
		if err != nil {
			t.Fatalf("NewImportRecord(...) = %v, want success", err)
		}

		resolved, err := r.ResolveOccurrenceMatch(importing.OccurrenceMatchResolutionDismissed)
		if err != nil {
			t.Fatalf("ResolveOccurrenceMatch(dismissed) = %v, want success", err)
		}
		om, ok := resolved.OccurrenceMatch()
		if !ok {
			t.Fatal("OccurrenceMatch() ok = false after ResolveOccurrenceMatch, want true")
		}
		if om.Resolution() != importing.OccurrenceMatchResolutionDismissed {
			t.Errorf("Resolution() = %q, want %q", om.Resolution(), importing.OccurrenceMatchResolutionDismissed)
		}
		// r itself must be unchanged (copy-transform, not mutation).
		if orig, _ := r.OccurrenceMatch(); orig.Resolved() {
			t.Error("original record's OccurrenceMatch Resolved() = true after ResolveOccurrenceMatch, want unchanged false")
		}
	})

	t.Run("rejects resolving a record with no occurrence match", func(t *testing.T) {
		t.Parallel()
		r := mustRecord(t)
		if _, err := r.ResolveOccurrenceMatch(importing.OccurrenceMatchResolutionDismissed); !errors.Is(err, importing.ErrImportRecordNoOccurrenceMatch) {
			t.Fatalf("ResolveOccurrenceMatch(...) error = %v, want ErrImportRecordNoOccurrenceMatch", err)
		}
	})
}

// TestImportRecord_SettleStatus covers issue #309's rule that a record
// only leaves ImportRecordStatusPending once every pending reason on it
// (an unresolved DuplicateMatch and/or an unresolved OccurrenceMatch) has
// cleared, and that exclusion wins over ready when the two disagree.
func TestImportRecord_SettleStatus(t *testing.T) {
	t.Parallel()

	newPendingRecord := func(t *testing.T, opts ...importing.ImportRecordOption) importing.ImportRecord {
		t.Helper()
		opts = append(opts, importing.WithRecordStatus(importing.ImportRecordStatusPending))
		r, err := importing.NewImportRecord(
			"record-1", "user-1", "batch-1", "raw", mustRecordDate(t, 2026, 8, 14), "desc",
			mustRecordMoney(t, -100, "USD"), 0, opts...,
		)
		if err != nil {
			t.Fatalf("NewImportRecord(...) = %v, want success", err)
		}
		return r
	}

	t.Run("no-op when not pending", func(t *testing.T) {
		t.Parallel()
		r := mustRecord(t) // pending, no matches at all
		ready, err := r.MarkReady()
		if err != nil {
			t.Fatalf("MarkReady() = %v, want success", err)
		}
		settled, err := ready.SettleStatus()
		if err != nil {
			t.Fatalf("SettleStatus() = %v, want success", err)
		}
		if settled.Status() != importing.ImportRecordStatusReady {
			t.Errorf("Status() = %q, want unchanged %q", settled.Status(), importing.ImportRecordStatusReady)
		}
	})

	t.Run("stays pending with an unresolved duplicate match and no occurrence match", func(t *testing.T) {
		t.Parallel()
		dm, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		r := newPendingRecord(t, importing.WithDuplicateMatch(dm))
		settled, err := r.SettleStatus()
		if err != nil {
			t.Fatalf("SettleStatus() = %v, want success", err)
		}
		if settled.Status() != importing.ImportRecordStatusPending {
			t.Errorf("Status() = %q, want %q", settled.Status(), importing.ImportRecordStatusPending)
		}
	})

	t.Run("stays pending with an unresolved occurrence match and no duplicate match", func(t *testing.T) {
		t.Parallel()
		om, err := importing.NewOccurrenceMatch("occ-1")
		if err != nil {
			t.Fatalf("NewOccurrenceMatch(...) = %v, want success", err)
		}
		r := newPendingRecord(t, importing.WithOccurrenceMatch(om))
		settled, err := r.SettleStatus()
		if err != nil {
			t.Fatalf("SettleStatus() = %v, want success", err)
		}
		if settled.Status() != importing.ImportRecordStatusPending {
			t.Errorf("Status() = %q, want %q", settled.Status(), importing.ImportRecordStatusPending)
		}
	})

	t.Run("moves to ready once a lone duplicate match is dismissed", func(t *testing.T) {
		t.Parallel()
		dm, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		dm, err = dm.Resolve(importing.DuplicateResolutionDismissed)
		if err != nil {
			t.Fatalf("Resolve(dismissed) = %v, want success", err)
		}
		r := newPendingRecord(t, importing.WithDuplicateMatch(dm))
		settled, err := r.SettleStatus()
		if err != nil {
			t.Fatalf("SettleStatus() = %v, want success", err)
		}
		if settled.Status() != importing.ImportRecordStatusReady {
			t.Errorf("Status() = %q, want %q", settled.Status(), importing.ImportRecordStatusReady)
		}
	})

	t.Run("moves to excluded once a lone occurrence match is materialized", func(t *testing.T) {
		t.Parallel()
		om, err := importing.NewOccurrenceMatch("occ-1")
		if err != nil {
			t.Fatalf("NewOccurrenceMatch(...) = %v, want success", err)
		}
		om, err = om.Resolve(importing.OccurrenceMatchResolutionMaterialized)
		if err != nil {
			t.Fatalf("Resolve(materialized) = %v, want success", err)
		}
		r := newPendingRecord(t, importing.WithOccurrenceMatch(om))
		settled, err := r.SettleStatus()
		if err != nil {
			t.Fatalf("SettleStatus() = %v, want success", err)
		}
		if settled.Status() != importing.ImportRecordStatusExcluded {
			t.Errorf("Status() = %q, want %q", settled.Status(), importing.ImportRecordStatusExcluded)
		}
	})

	t.Run("stays pending when only one of two reasons has resolved", func(t *testing.T) {
		t.Parallel()
		dm, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		dm, err = dm.Resolve(importing.DuplicateResolutionDismissed)
		if err != nil {
			t.Fatalf("Resolve(dismissed) = %v, want success", err)
		}
		om, err := importing.NewOccurrenceMatch("occ-1") // still unresolved
		if err != nil {
			t.Fatalf("NewOccurrenceMatch(...) = %v, want success", err)
		}
		r := newPendingRecord(t, importing.WithDuplicateMatch(dm), importing.WithOccurrenceMatch(om))
		settled, err := r.SettleStatus()
		if err != nil {
			t.Fatalf("SettleStatus() = %v, want success", err)
		}
		if settled.Status() != importing.ImportRecordStatusPending {
			t.Errorf("Status() = %q, want %q — the occurrence match is still unresolved", settled.Status(), importing.ImportRecordStatusPending)
		}
	})

	t.Run("excluded wins over ready when both reasons resolve and disagree", func(t *testing.T) {
		t.Parallel()
		dm, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		dm, err = dm.Resolve(importing.DuplicateResolutionDismissed) // says "proceed"
		if err != nil {
			t.Fatalf("Resolve(dismissed) = %v, want success", err)
		}
		om, err := importing.NewOccurrenceMatch("occ-1")
		if err != nil {
			t.Fatalf("NewOccurrenceMatch(...) = %v, want success", err)
		}
		om, err = om.Resolve(importing.OccurrenceMatchResolutionMaterialized) // says "exclude"
		if err != nil {
			t.Fatalf("Resolve(materialized) = %v, want success", err)
		}
		r := newPendingRecord(t, importing.WithDuplicateMatch(dm), importing.WithOccurrenceMatch(om))
		settled, err := r.SettleStatus()
		if err != nil {
			t.Fatalf("SettleStatus() = %v, want success", err)
		}
		if settled.Status() != importing.ImportRecordStatusExcluded {
			t.Errorf("Status() = %q, want %q — a materialized occurrence match must win over a dismissed duplicate match", settled.Status(), importing.ImportRecordStatusExcluded)
		}
	})

	t.Run("ready when both reasons resolve in agreement", func(t *testing.T) {
		t.Parallel()
		dm, err := importing.NewDuplicateMatch(importing.DuplicateMatchTierSuspected, "txn-1")
		if err != nil {
			t.Fatalf("NewDuplicateMatch(...) = %v, want success", err)
		}
		dm, err = dm.Resolve(importing.DuplicateResolutionDismissed)
		if err != nil {
			t.Fatalf("Resolve(dismissed) = %v, want success", err)
		}
		om, err := importing.NewOccurrenceMatch("occ-1")
		if err != nil {
			t.Fatalf("NewOccurrenceMatch(...) = %v, want success", err)
		}
		om, err = om.Resolve(importing.OccurrenceMatchResolutionDismissed)
		if err != nil {
			t.Fatalf("Resolve(dismissed) = %v, want success", err)
		}
		r := newPendingRecord(t, importing.WithDuplicateMatch(dm), importing.WithOccurrenceMatch(om))
		settled, err := r.SettleStatus()
		if err != nil {
			t.Fatalf("SettleStatus() = %v, want success", err)
		}
		if settled.Status() != importing.ImportRecordStatusReady {
			t.Errorf("Status() = %q, want %q", settled.Status(), importing.ImportRecordStatusReady)
		}
	})
}
