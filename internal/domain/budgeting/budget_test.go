package budgeting_test

import (
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/budgeting"
)

func mustDate(t *testing.T, y int, m time.Month, d int) domain.Date {
	t.Helper()
	date, err := domain.NewDate(y, m, d)
	if err != nil {
		t.Fatalf("domain.NewDate: %v", err)
	}
	return date
}

func mustLine(t *testing.T, budgetID, categoryID string, amountMinor int64) budgeting.BudgetLine {
	t.Helper()
	l, err := budgeting.NewBudgetLine("line-"+categoryID, budgetID, categoryID, amountMinor, false)
	if err != nil {
		t.Fatalf("budgeting.NewBudgetLine: %v", err)
	}
	return l
}

func TestNewBudget(t *testing.T) {
	startsOn := mustDate(t, 2026, time.January, 1)
	lines := []budgeting.BudgetLine{mustLine(t, "b1", "cat-groceries", 50000)}

	b, err := budgeting.NewBudget("b1", "user-1", "Household", budgeting.PeriodTypeMonthly, "USD", startsOn, lines)
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	if b.ID() != "b1" || b.UserID() != "user-1" || b.Name() != "Household" ||
		b.PeriodType() != budgeting.PeriodTypeMonthly || b.Currency() != "USD" || b.StartsOn() != startsOn {
		t.Errorf("NewBudget() = %+v, unexpected field values", b)
	}
	if b.Archived() {
		t.Error("a freshly constructed budget must not be archived")
	}
	if got := b.Lines(); len(got) != 1 || got[0].CategoryID() != "cat-groceries" {
		t.Errorf("Lines() = %+v, want the one seeded line", got)
	}
}

func TestNewBudget_EmptyFields(t *testing.T) {
	startsOn := mustDate(t, 2026, time.January, 1)

	tests := []struct {
		name     string
		id       string
		userID   string
		budName  string
		currency string
		wantErr  error
	}{
		{"empty id", "", "user-1", "Household", "USD", budgeting.ErrBudgetEmptyID},
		{"empty user id", "b1", "", "Household", "USD", budgeting.ErrBudgetEmptyUserID},
		{"empty name", "b1", "user-1", "", "USD", budgeting.ErrBudgetEmptyName},
		{"invalid currency", "b1", "user-1", "Household", "NOTACODE", budgeting.ErrBudgetInvalidCurrency},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := budgeting.NewBudget(tt.id, tt.userID, tt.budName, budgeting.PeriodTypeMonthly, tt.currency, startsOn, nil)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("NewBudget() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewBudget_InvalidPeriodType(t *testing.T) {
	startsOn := mustDate(t, 2026, time.January, 1)
	_, err := budgeting.NewBudget("b1", "user-1", "Household", budgeting.PeriodType("weekly"), "USD", startsOn, nil)
	if !errors.Is(err, budgeting.ErrBudgetInvalidPeriodType) {
		t.Errorf("NewBudget() error = %v, want ErrBudgetInvalidPeriodType", err)
	}
}

func TestNewBudget_RejectsDuplicateCategoryLine(t *testing.T) {
	startsOn := mustDate(t, 2026, time.January, 1)
	lines := []budgeting.BudgetLine{
		mustLine(t, "b1", "cat-groceries", 50000),
		mustLine(t, "b1", "cat-groceries", 10000),
	}
	_, err := budgeting.NewBudget("b1", "user-1", "Household", budgeting.PeriodTypeMonthly, "USD", startsOn, lines)
	if !errors.Is(err, budgeting.ErrBudgetDuplicateCategoryLine) {
		t.Errorf("NewBudget() error = %v, want ErrBudgetDuplicateCategoryLine", err)
	}
}

func TestBudget_WithArchivedAt(t *testing.T) {
	startsOn := mustDate(t, 2026, time.January, 1)
	archivedOn := mustDate(t, 2026, time.June, 1)

	b, err := budgeting.NewBudget("b1", "user-1", "Household", budgeting.PeriodTypeMonthly, "USD", startsOn, nil,
		budgeting.WithArchivedAt(archivedOn))
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	if !b.Archived() {
		t.Error("Archived() = false, want true after WithArchivedAt")
	}
	got, ok := b.ArchivedAt()
	if !ok || got != archivedOn {
		t.Errorf("ArchivedAt() = (%v, %v), want (%v, true)", got, ok, archivedOn)
	}
}

func TestBudget_Lines_ReturnsCopy(t *testing.T) {
	startsOn := mustDate(t, 2026, time.January, 1)
	lines := []budgeting.BudgetLine{mustLine(t, "b1", "cat-groceries", 50000)}
	b, err := budgeting.NewBudget("b1", "user-1", "Household", budgeting.PeriodTypeMonthly, "USD", startsOn, lines)
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}

	got := b.Lines()
	got[0] = mustLine(t, "b1", "cat-rent", 100000)

	again := b.Lines()
	if again[0].CategoryID() != "cat-groceries" {
		t.Error("mutating a returned Lines() slice must not affect the budget's own state")
	}
}
