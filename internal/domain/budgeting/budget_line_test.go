package budgeting_test

import (
	"errors"
	"testing"

	"github.com/anirudhgray/bodger/internal/domain/budgeting"
)

func TestNewBudgetLine(t *testing.T) {
	l, err := budgeting.NewBudgetLine("line-1", "budget-1", "cat-groceries", 50000, true)
	if err != nil {
		t.Fatalf("NewBudgetLine: %v", err)
	}
	if l.ID() != "line-1" || l.BudgetID() != "budget-1" || l.CategoryID() != "cat-groceries" ||
		l.AmountMinor() != 50000 || !l.Rollover() {
		t.Errorf("NewBudgetLine() = %+v, unexpected field values", l)
	}
}

func TestNewBudgetLine_Invalid(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		budgetID    string
		categoryID  string
		amountMinor int64
		wantErr     error
	}{
		{"empty id", "", "budget-1", "cat-groceries", 50000, budgeting.ErrBudgetLineEmptyID},
		{"empty budget id", "line-1", "", "cat-groceries", 50000, budgeting.ErrBudgetLineEmptyBudgetID},
		{"empty category id", "line-1", "budget-1", "", 50000, budgeting.ErrBudgetLineEmptyCategoryID},
		{"zero amount", "line-1", "budget-1", "cat-groceries", 0, budgeting.ErrBudgetLineAmountNotPositive},
		{"negative amount", "line-1", "budget-1", "cat-groceries", -1, budgeting.ErrBudgetLineAmountNotPositive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := budgeting.NewBudgetLine(tt.id, tt.budgetID, tt.categoryID, tt.amountMinor, false)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("NewBudgetLine() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
