package recurring_test

import (
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain/recurring"
)

func TestNewScheduledOccurrence(t *testing.T) {
	on := mustDate(t, 2026, time.February, 28)

	o, err := recurring.NewScheduledOccurrence("occ-1", "rule-1", on)
	if err != nil {
		t.Fatalf("NewScheduledOccurrence: %v", err)
	}
	if o.ID() != "occ-1" || o.RuleID() != "rule-1" || o.OccurrenceDate() != on {
		t.Errorf("NewScheduledOccurrence() = %+v, unexpected field values", o)
	}
	if o.Status() != recurring.OccurrenceStatusPending {
		t.Errorf("Status() = %q, want %q", o.Status(), recurring.OccurrenceStatusPending)
	}
	if _, ok := o.TransactionID(); ok {
		t.Error("a freshly constructed occurrence must carry no transaction id")
	}
}

func TestNewScheduledOccurrence_Validation(t *testing.T) {
	on := mustDate(t, 2026, time.February, 28)

	tests := []struct {
		name    string
		id      string
		ruleID  string
		opts    []recurring.ScheduledOccurrenceOption
		wantErr error
	}{
		{
			name: "empty id", id: "", ruleID: "rule-1",
			wantErr: recurring.ErrOccurrenceEmptyID,
		},
		{
			name: "empty rule id", id: "occ-1", ruleID: "",
			wantErr: recurring.ErrOccurrenceEmptyRuleID,
		},
		{
			name: "unknown status", id: "occ-1", ruleID: "rule-1",
			opts:    []recurring.ScheduledOccurrenceOption{recurring.WithOccurrenceStatus(recurring.OccurrenceStatus("bogus"))},
			wantErr: recurring.ErrOccurrenceInvalidStatus,
		},
		{
			name: "materialised without a transaction", id: "occ-1", ruleID: "rule-1",
			opts:    []recurring.ScheduledOccurrenceOption{recurring.WithOccurrenceStatus(recurring.OccurrenceStatusMaterialised)},
			wantErr: recurring.ErrOccurrenceEmptyTransactionID,
		},
		{
			name: "pending with a transaction", id: "occ-1", ruleID: "rule-1",
			opts:    []recurring.ScheduledOccurrenceOption{recurring.WithTransactionID("txn-1")},
			wantErr: recurring.ErrOccurrenceUnexpectedTransactionID,
		},
		{
			name: "skipped with a transaction", id: "occ-1", ruleID: "rule-1",
			opts: []recurring.ScheduledOccurrenceOption{
				recurring.WithOccurrenceStatus(recurring.OccurrenceStatusSkipped),
				recurring.WithTransactionID("txn-1"),
			},
			wantErr: recurring.ErrOccurrenceUnexpectedTransactionID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := recurring.NewScheduledOccurrence(tt.id, tt.ruleID, on, tt.opts...)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("NewScheduledOccurrence() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestNewScheduledOccurrence_ReconstructsMaterialised is the sqlite
// adapter's restore path: an already-persisted materialised row comes back
// with both its status and its transaction ID.
func TestNewScheduledOccurrence_ReconstructsMaterialised(t *testing.T) {
	o, err := recurring.NewScheduledOccurrence("occ-1", "rule-1", mustDate(t, 2026, time.March, 31),
		recurring.WithOccurrenceStatus(recurring.OccurrenceStatusMaterialised),
		recurring.WithTransactionID("txn-1"))
	if err != nil {
		t.Fatalf("NewScheduledOccurrence: %v", err)
	}
	if o.Status() != recurring.OccurrenceStatusMaterialised {
		t.Errorf("Status() = %q, want %q", o.Status(), recurring.OccurrenceStatusMaterialised)
	}
	if got, ok := o.TransactionID(); !ok || got != "txn-1" {
		t.Errorf("TransactionID() = (%q, %v), want (\"txn-1\", true)", got, ok)
	}
}

func TestScheduledOccurrence_MarkMaterialised(t *testing.T) {
	pending, err := recurring.NewScheduledOccurrence("occ-1", "rule-1", mustDate(t, 2026, time.March, 31))
	if err != nil {
		t.Fatalf("NewScheduledOccurrence: %v", err)
	}

	materialised, err := pending.MarkMaterialised("txn-1")
	if err != nil {
		t.Fatalf("MarkMaterialised: %v", err)
	}
	if materialised.Status() != recurring.OccurrenceStatusMaterialised {
		t.Errorf("Status() = %q, want %q", materialised.Status(), recurring.OccurrenceStatusMaterialised)
	}
	if got, ok := materialised.TransactionID(); !ok || got != "txn-1" {
		t.Errorf("TransactionID() = (%q, %v), want (\"txn-1\", true)", got, ok)
	}
	if pending.Status() != recurring.OccurrenceStatusPending {
		t.Errorf("the original occurrence's Status() = %q after MarkMaterialised, want an unchanged %q",
			pending.Status(), recurring.OccurrenceStatusPending)
	}
	if _, ok := pending.TransactionID(); ok {
		t.Error("the original occurrence gained a transaction id from a copy-transform")
	}
}

func TestScheduledOccurrence_MarkMaterialised_RequiresATransaction(t *testing.T) {
	pending, err := recurring.NewScheduledOccurrence("occ-1", "rule-1", mustDate(t, 2026, time.March, 31))
	if err != nil {
		t.Fatalf("NewScheduledOccurrence: %v", err)
	}
	if _, err := pending.MarkMaterialised(""); !errors.Is(err, recurring.ErrOccurrenceEmptyTransactionID) {
		t.Errorf("MarkMaterialised(\"\") error = %v, want ErrOccurrenceEmptyTransactionID", err)
	}
}

func TestScheduledOccurrence_MarkSkipped(t *testing.T) {
	pending, err := recurring.NewScheduledOccurrence("occ-1", "rule-1", mustDate(t, 2026, time.March, 31))
	if err != nil {
		t.Fatalf("NewScheduledOccurrence: %v", err)
	}

	skipped, err := pending.MarkSkipped()
	if err != nil {
		t.Fatalf("MarkSkipped: %v", err)
	}
	if skipped.Status() != recurring.OccurrenceStatusSkipped {
		t.Errorf("Status() = %q, want %q", skipped.Status(), recurring.OccurrenceStatusSkipped)
	}
	if _, ok := skipped.TransactionID(); ok {
		t.Error("a skipped occurrence must carry no transaction id")
	}
	if pending.Status() != recurring.OccurrenceStatusPending {
		t.Errorf("the original occurrence's Status() = %q after MarkSkipped, want unchanged", pending.Status())
	}
}

// TestScheduledOccurrence_TerminalStatusesAreTerminal pins the state
// machine: materialised and skipped are both ends of the road, in either
// direction.
func TestScheduledOccurrence_TerminalStatusesAreTerminal(t *testing.T) {
	on := mustDate(t, 2026, time.March, 31)

	materialised, err := recurring.NewScheduledOccurrence("occ-1", "rule-1", on,
		recurring.WithOccurrenceStatus(recurring.OccurrenceStatusMaterialised),
		recurring.WithTransactionID("txn-1"))
	if err != nil {
		t.Fatalf("NewScheduledOccurrence(materialised): %v", err)
	}
	skipped, err := recurring.NewScheduledOccurrence("occ-2", "rule-1", on,
		recurring.WithOccurrenceStatus(recurring.OccurrenceStatusSkipped))
	if err != nil {
		t.Fatalf("NewScheduledOccurrence(skipped): %v", err)
	}

	if _, err := materialised.MarkSkipped(); !errors.Is(err, recurring.ErrOccurrenceInvalidTransition) {
		t.Errorf("materialised -> skipped error = %v, want ErrOccurrenceInvalidTransition", err)
	}
	if _, err := materialised.MarkMaterialised("txn-2"); !errors.Is(err, recurring.ErrOccurrenceInvalidTransition) {
		t.Errorf("materialised -> materialised error = %v, want ErrOccurrenceInvalidTransition", err)
	}
	if _, err := skipped.MarkMaterialised("txn-1"); !errors.Is(err, recurring.ErrOccurrenceInvalidTransition) {
		t.Errorf("skipped -> materialised error = %v, want ErrOccurrenceInvalidTransition", err)
	}
	if _, err := skipped.MarkSkipped(); !errors.Is(err, recurring.ErrOccurrenceInvalidTransition) {
		t.Errorf("skipped -> skipped error = %v, want ErrOccurrenceInvalidTransition", err)
	}
}
