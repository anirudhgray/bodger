package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

func mustOccurrence(t *testing.T, id, ruleID string, on domain.Date, opts ...recurring.ScheduledOccurrenceOption) recurring.ScheduledOccurrence {
	t.Helper()
	o, err := recurring.NewScheduledOccurrence(id, ruleID, on, opts...)
	if err != nil {
		t.Fatalf("recurring.NewScheduledOccurrence: %v", err)
	}
	return o
}

// seedRule creates the account, category, and rule an occurrence needs,
// and returns the rule's ID.
func seedRule(t *testing.T, db *DB, userID, ruleID string) string {
	t.Helper()
	seedAccountAndCategory(t, db, userID, ruleID+"-account", ruleID+"-category")
	rule := mustRule(t, ruleID, userID, ruleID+"-account", ruleID+"-category", mustMonthlySchedule(t, 1, 15))
	if err := NewRecurringRuleRepository(db).Create(context.Background(), userID, rule); err != nil {
		t.Fatalf("seed rule %q: %v", ruleID, err)
	}
	return ruleID
}

// seedTransactionForOccurrence creates a real transaction satisfying
// scheduled_occurrences.transaction_id's foreign key, for the
// materialisation paths. Nothing in this package materialises an
// occurrence itself — that use case is issue #279 — so the transaction is
// created directly through the ledger's own repository, which is exactly
// the point: the money is the transaction, never the occurrence.
// It posts to whichever account and category seedRule created for ruleID.
func seedTransactionForOccurrence(t *testing.T, db *DB, userID, ruleID string) string {
	t.Helper()
	categoryID := ruleID + "-category"
	posting := mustPosting(t, "post-materialised", ruleID+"-account", -50000, "USD", &categoryID)
	txn, err := ledger.NewOutflow("txn-materialised", userID, mustDate(t, 2026, time.January, 15), "Rent",
		[]ledger.Posting{posting})
	if err != nil {
		t.Fatalf("ledger.NewOutflow: %v", err)
	}
	if err := NewTransactionRepository(db).Create(context.Background(), userID, txn, nil); err != nil {
		t.Fatalf("seed transaction: %v", err)
	}
	return txn.ID()
}

func occurrenceDates(occurrences []recurring.ScheduledOccurrence) []string {
	out := make([]string, len(occurrences))
	for i, o := range occurrences {
		out[i] = o.OccurrenceDate().String()
	}
	return out
}

func TestScheduledOccurrenceRepository_CreateBatchGet(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	occurrences := []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, mustDate(t, 2026, time.January, 15)),
		mustOccurrence(t, "occ-2", ruleID, mustDate(t, 2026, time.February, 15)),
	}
	if err := repo.CreateBatch(ctx, ports.SeededUserID, occurrences); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "occ-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != "occ-1" || got.RuleID() != ruleID || got.OccurrenceDate().String() != "2026-01-15" {
		t.Errorf("Get() = %+v, want a round trip of the first occurrence", got)
	}
	if got.Status() != recurring.OccurrenceStatusPending {
		t.Errorf("Status() = %q, want %q", got.Status(), recurring.OccurrenceStatusPending)
	}
	if _, ok := got.TransactionID(); ok {
		t.Error("a pending occurrence must read back with no transaction id")
	}
}

func TestScheduledOccurrenceRepository_CreateBatch_Empty(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewScheduledOccurrenceRepository(db)
	if err := repo.CreateBatch(context.Background(), ports.SeededUserID, nil); err != nil {
		t.Fatalf("CreateBatch(nil) = %v, want no error", err)
	}
}

// TestScheduledOccurrenceRepository_CreateBatch_IsIdempotentPerDate is
// ADR-0014's generation-safety guarantee: the UNIQUE (rule_id,
// occurrence_date) constraint means re-running generation over an
// overlapping window can never duplicate a firing.
func TestScheduledOccurrenceRepository_CreateBatch_IsIdempotentPerDate(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	on := mustDate(t, 2026, time.January, 15)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, on),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-2", ruleID, on),
	})
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.Conflict {
		t.Fatalf("CreateBatch(duplicate date) error = %v, want *errs.Error with code Conflict", err)
	}
}

// TestScheduledOccurrenceRepository_CreateBatch_IsAtomic proves a batch
// that fails partway writes nothing, the same all-or-nothing contract
// ImportRecordRepository.CreateBatch has.
func TestScheduledOccurrenceRepository_CreateBatch_IsAtomic(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	on := mustDate(t, 2026, time.January, 15)
	err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, mustDate(t, 2026, time.February, 15)),
		// Same date twice in one batch — the second insert violates the
		// UNIQUE constraint the first one just satisfied.
		mustOccurrence(t, "occ-2", ruleID, on),
		mustOccurrence(t, "occ-3", ruleID, on),
	})
	if err == nil {
		t.Fatal("CreateBatch with a duplicate date inside the batch returned no error")
	}

	got, err := repo.List(ctx, ports.SeededUserID, ports.ScheduledOccurrenceFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a failed batch left %d occurrences behind: %v", len(got), occurrenceDates(got))
	}
}

func TestScheduledOccurrenceRepository_CreateBatch_RejectsAnotherUsersRule(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	err := repo.CreateBatch(ctx, otherUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, mustDate(t, 2026, time.January, 15)),
	})
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotAllowed {
		t.Fatalf("CreateBatch(other user's rule) error = %v, want *errs.Error with code NotAllowed", err)
	}
}

func TestScheduledOccurrenceRepository_CreateBatch_RejectsUnknownRule(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewScheduledOccurrenceRepository(db)
	err := repo.CreateBatch(context.Background(), ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", "no-such-rule", mustDate(t, 2026, time.January, 15)),
	})
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("CreateBatch(unknown rule) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestScheduledOccurrenceRepository_Get_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewScheduledOccurrenceRepository(db)
	_, err := repo.Get(context.Background(), ports.SeededUserID, "no-such-occurrence")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Get(unknown) error = %v, want *errs.Error with code NotFound", err)
	}
}

// TestScheduledOccurrenceRepository_Get_CrossUserIsolation exercises the
// scope-through-the-rule ownership path: an occurrence carries no user_id
// of its own, so isolation depends entirely on the join (ADR-0014).
func TestScheduledOccurrenceRepository_Get_CrossUserIsolation(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, mustDate(t, 2026, time.January, 15)),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	_, err := repo.Get(ctx, otherUserID, "occ-1")
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Get(other user's occurrence) error = %v, want *errs.Error with code NotFound", err)
	}

	got, err := repo.List(ctx, otherUserID, ports.ScheduledOccurrenceFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List for another user returned %d occurrences, want none", len(got))
	}
}

func TestScheduledOccurrenceRepository_List_Filters(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	ruleA := seedRule(t, db, ports.SeededUserID, "rule-a")
	ruleB := seedRule(t, db, ports.SeededUserID, "rule-b")

	repo := NewScheduledOccurrenceRepository(db)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-a-mar", ruleA, mustDate(t, 2026, time.March, 15)),
		mustOccurrence(t, "occ-a-jan", ruleA, mustDate(t, 2026, time.January, 15)),
		mustOccurrence(t, "occ-a-feb", ruleA, mustDate(t, 2026, time.February, 15),
			recurring.WithOccurrenceStatus(recurring.OccurrenceStatusSkipped)),
		mustOccurrence(t, "occ-b-jan", ruleB, mustDate(t, 2026, time.January, 20)),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	from := mustDate(t, 2026, time.January, 16)
	to := mustDate(t, 2026, time.February, 28)

	tests := []struct {
		name   string
		filter ports.ScheduledOccurrenceFilter
		want   []string
	}{
		{
			name:   "no filter lists every occurrence in date order",
			filter: ports.ScheduledOccurrenceFilter{},
			want:   []string{"2026-01-15", "2026-01-20", "2026-02-15", "2026-03-15"},
		},
		{
			name:   "by rule",
			filter: ports.ScheduledOccurrenceFilter{RuleID: ruleB},
			want:   []string{"2026-01-20"},
		},
		{
			name:   "by status",
			filter: ports.ScheduledOccurrenceFilter{Status: recurring.OccurrenceStatusPending},
			want:   []string{"2026-01-15", "2026-01-20", "2026-03-15"},
		},
		{
			name:   "by date range, inclusive at both ends",
			filter: ports.ScheduledOccurrenceFilter{FromDate: &from, ToDate: &to},
			want:   []string{"2026-01-20", "2026-02-15"},
		},
		{
			name: "pending in a date range — the query issues #278-#280 run",
			filter: ports.ScheduledOccurrenceFilter{
				Status: recurring.OccurrenceStatusPending, FromDate: &from, ToDate: &to,
			},
			want: []string{"2026-01-20"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.List(ctx, ports.SeededUserID, tt.filter)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			dates := occurrenceDates(got)
			if len(dates) != len(tt.want) {
				t.Fatalf("List() = %v, want %v", dates, tt.want)
			}
			for i := range tt.want {
				if dates[i] != tt.want[i] {
					t.Fatalf("List() = %v, want %v", dates, tt.want)
				}
			}
		})
	}
}

// TestScheduledOccurrenceRepository_Update_Materialise persists the
// pending -> materialised transition, including the transaction the
// occurrence produced.
func TestScheduledOccurrenceRepository_Update_Materialise(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")
	txnID := seedTransactionForOccurrence(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	on := mustDate(t, 2026, time.January, 15)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, on),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	pending, err := repo.Get(ctx, ports.SeededUserID, "occ-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	materialised, err := pending.MarkMaterialised(txnID)
	if err != nil {
		t.Fatalf("MarkMaterialised: %v", err)
	}
	if err := repo.Update(ctx, ports.SeededUserID, materialised); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "occ-1")
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if got.Status() != recurring.OccurrenceStatusMaterialised {
		t.Errorf("Status() = %q, want %q", got.Status(), recurring.OccurrenceStatusMaterialised)
	}
	if gotTxn, ok := got.TransactionID(); !ok || gotTxn != txnID {
		t.Errorf("TransactionID() = (%q, %v), want (%q, true)", gotTxn, ok, txnID)
	}
}

func TestScheduledOccurrenceRepository_Update_Skip(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, mustDate(t, 2026, time.January, 15)),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	pending, err := repo.Get(ctx, ports.SeededUserID, "occ-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	skipped, err := pending.MarkSkipped()
	if err != nil {
		t.Fatalf("MarkSkipped: %v", err)
	}
	if err := repo.Update(ctx, ports.SeededUserID, skipped); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.Get(ctx, ports.SeededUserID, "occ-1")
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if got.Status() != recurring.OccurrenceStatusSkipped {
		t.Errorf("Status() = %q, want %q", got.Status(), recurring.OccurrenceStatusSkipped)
	}
}

func TestScheduledOccurrenceRepository_Update_NotFound(t *testing.T) {
	db, _ := newTestDB(t)
	repo := NewScheduledOccurrenceRepository(db)
	err := repo.Update(context.Background(), ports.SeededUserID,
		mustOccurrence(t, "no-such-occurrence", "r1", mustDate(t, 2026, time.January, 15)))
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Update(unknown) error = %v, want *errs.Error with code NotFound", err)
	}
}

func TestScheduledOccurrenceRepository_Update_CrossUserIsolation(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	occurrence := mustOccurrence(t, "occ-1", ruleID, mustDate(t, 2026, time.January, 15))
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{occurrence}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	skipped, err := occurrence.MarkSkipped()
	if err != nil {
		t.Fatalf("MarkSkipped: %v", err)
	}
	err = repo.Update(ctx, otherUserID, skipped)
	var e *errs.Error
	if !errors.As(err, &e) || e.Code != errs.NotFound {
		t.Fatalf("Update(other user's occurrence) error = %v, want *errs.Error with code NotFound", err)
	}
}

// TestScheduledOccurrenceRepository_DeletePending is the regeneration path
// a rule's schedule change needs (issue #277), and the guarantee that
// matters most about it: a materialised or skipped occurrence is never
// swept away, and neither is a pending one before the cutoff date.
func TestScheduledOccurrenceRepository_DeletePending(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")
	otherRuleID := seedRule(t, db, ports.SeededUserID, "r2")
	txnID := seedTransactionForOccurrence(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-past", ruleID, mustDate(t, 2026, time.January, 15)),
		mustOccurrence(t, "occ-future-pending", ruleID, mustDate(t, 2026, time.April, 15)),
		mustOccurrence(t, "occ-future-skipped", ruleID, mustDate(t, 2026, time.May, 15),
			recurring.WithOccurrenceStatus(recurring.OccurrenceStatusSkipped)),
		mustOccurrence(t, "occ-future-materialised", ruleID, mustDate(t, 2026, time.June, 15),
			recurring.WithOccurrenceStatus(recurring.OccurrenceStatusMaterialised),
			recurring.WithTransactionID(txnID)),
		mustOccurrence(t, "occ-other-rule", otherRuleID, mustDate(t, 2026, time.April, 20)),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	if err := repo.DeletePending(ctx, ports.SeededUserID, ruleID, mustDate(t, 2026, time.March, 1)); err != nil {
		t.Fatalf("DeletePending: %v", err)
	}

	got, err := repo.List(ctx, ports.SeededUserID, ports.ScheduledOccurrenceFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"2026-01-15", "2026-04-20", "2026-05-15", "2026-06-15"}
	dates := occurrenceDates(got)
	if len(dates) != len(want) {
		t.Fatalf("after DeletePending, occurrences = %v, want %v", dates, want)
	}
	for i := range want {
		if dates[i] != want[i] {
			t.Fatalf("after DeletePending, occurrences = %v, want %v", dates, want)
		}
	}
}

func TestScheduledOccurrenceRepository_DeletePending_CrossUserIsolation(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	seedOtherUser(t, db, otherUserID)
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, mustDate(t, 2026, time.April, 15)),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	if err := repo.DeletePending(ctx, otherUserID, ruleID, mustDate(t, 2026, time.January, 1)); err != nil {
		t.Fatalf("DeletePending: %v", err)
	}

	got, err := repo.List(ctx, ports.SeededUserID, ports.ScheduledOccurrenceFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("another user's DeletePending removed %d of the owner's occurrences", 1-len(got))
	}
}

// TestScheduledOccurrences_NeverReachABalance is the blunt version of
// data-model.md §11's invariant, asserted against the real schema: rules
// and pending occurrences exist, and the ledger is still completely empty.
// Nothing here creates a posting, and there is no column on either new
// table that a balance query could join through to an account.
func TestScheduledOccurrences_NeverReachABalance(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, mustDate(t, 2026, time.January, 15)),
		mustOccurrence(t, "occ-2", ruleID, mustDate(t, 2026, time.February, 15)),
		mustOccurrence(t, "occ-3", ruleID, mustDate(t, 2026, time.March, 15)),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	for _, table := range []string{"postings", "transactions"} {
		var count int
		if err := db.read.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Errorf("%d rows in %s after creating only rules and occurrences — a projection reached the ledger", count, table)
		}
	}

	txns, err := NewTransactionRepository(db).List(ctx, ports.SeededUserID, ports.TransactionFilter{})
	if err != nil {
		t.Fatalf("TransactionRepository.List: %v", err)
	}
	if len(txns) != 0 {
		t.Errorf("TransactionRepository.List returned %d transactions, want none", len(txns))
	}
}

// TestScheduledOccurrences_CascadeWithTheirRule mirrors budget_lines'
// cascade on its budget: an occurrence has no independent existence once
// its rule is gone.
func TestScheduledOccurrences_CascadeWithTheirRule(t *testing.T) {
	db, _ := newTestDB(t)
	ctx := context.Background()
	ruleID := seedRule(t, db, ports.SeededUserID, "r1")

	repo := NewScheduledOccurrenceRepository(db)
	if err := repo.CreateBatch(ctx, ports.SeededUserID, []recurring.ScheduledOccurrence{
		mustOccurrence(t, "occ-1", ruleID, mustDate(t, 2026, time.January, 15)),
	}); err != nil {
		t.Fatalf("CreateBatch: %v", err)
	}

	// There is no rule-deletion use case (rules are archived, not
	// deleted); this goes straight to SQL to prove the constraint itself.
	if _, err := db.write.ExecContext(ctx, `DELETE FROM recurring_rules WHERE id = ?`, ruleID); err != nil {
		t.Fatalf("delete rule: %v", err)
	}

	var count int
	if err := db.read.QueryRowContext(ctx, `SELECT count(*) FROM scheduled_occurrences`).Scan(&count); err != nil {
		t.Fatalf("count occurrences: %v", err)
	}
	if count != 0 {
		t.Errorf("%d occurrences survived their rule's deletion, want 0", count)
	}
}
