package importparse_test

import (
	"errors"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app/importparse"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

var testClock = clock.NewFrozen(time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC))

func mapping() importparse.ColumnMapping {
	return importparse.ColumnMapping{
		DateColumn:        "Date",
		DescriptionColumn: "Description",
		AmountColumn:      "Amount",
	}
}

func fullMapping() importparse.ColumnMapping {
	return importparse.ColumnMapping{
		DateColumn:        "Date",
		DescriptionColumn: "Description",
		AmountColumn:      "Amount",
		PostedDateColumn:  "Posted",
		CurrencyColumn:    "Currency",
		ExternalIDColumn:  "Ref",
		CategoryColumn:    "Category",
	}
}

func wantErrCode(t *testing.T, err error, code errs.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("want an error with code %s, got nil", code)
	}
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want a *errs.Error with code %s", err, code)
	}
	if e.Code != code {
		t.Errorf("err code = %s, want %s (message: %s)", e.Code, code, e.Message)
	}
}

func TestNewCSVParser_ValidatesMapping(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		mapping  importparse.ColumnMapping
		currency string
	}{
		{"missing date column", importparse.ColumnMapping{DescriptionColumn: "Description", AmountColumn: "Amount"}, "USD"},
		{"missing description column", importparse.ColumnMapping{DateColumn: "Date", AmountColumn: "Amount"}, "USD"},
		{"missing amount column", importparse.ColumnMapping{DateColumn: "Date", DescriptionColumn: "Description"}, "USD"},
		{"unknown fallback currency", mapping(), "XXX"},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := importparse.NewCSVParser(tt.mapping, tt.currency, testClock, "UTC")
			wantErrCode(t, err, errs.InvalidInput)
		})
	}
}

func TestCSVParser_Parse_BasicFile(t *testing.T) {
	t.Parallel()
	p, err := importparse.NewCSVParser(mapping(), "USD", testClock, "UTC")
	if err != nil {
		t.Fatalf("NewCSVParser: %v", err)
	}

	data := "Date,Description,Amount\n2026-08-01,Coffee Shop,-4.50\n2026-08-02,Paycheck,1500.00\n"
	rows, err := p.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("Parse() returned %d rows, want 2", len(rows))
	}

	if rows[0].Description != "Coffee Shop" {
		t.Errorf("rows[0].Description = %q, want %q", rows[0].Description, "Coffee Shop")
	}
	if rows[0].Amount.AmountMinor() != -450 || rows[0].Amount.Currency() != "USD" {
		t.Errorf("rows[0].Amount = %s, want -4.50 USD", rows[0].Amount)
	}
	if rows[0].BookedDate.Year() != 2026 || rows[0].BookedDate.Month() != time.August || rows[0].BookedDate.Day() != 1 {
		t.Errorf("rows[0].BookedDate = %s, want 2026-08-01", rows[0].BookedDate)
	}
	if rows[0].SortOrder != 0 {
		t.Errorf("rows[0].SortOrder = %d, want 0", rows[0].SortOrder)
	}
	if rows[1].SortOrder != 1 {
		t.Errorf("rows[1].SortOrder = %d, want 1", rows[1].SortOrder)
	}
	if rows[1].Amount.AmountMinor() != 150000 {
		t.Errorf("rows[1].Amount minor = %d, want 150000", rows[1].Amount.AmountMinor())
	}
}

func TestCSVParser_Parse_OptionalColumns(t *testing.T) {
	t.Parallel()
	p, err := importparse.NewCSVParser(fullMapping(), "USD", testClock, "UTC")
	if err != nil {
		t.Fatalf("NewCSVParser: %v", err)
	}

	data := "Date,Description,Amount,Posted,Currency,Ref,Category\n" +
		"2026-08-01,Coffee Shop,-4.50,2026-08-02,EUR,ext-123,Dining\n"
	rows, err := p.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Parse() returned %d rows, want 1", len(rows))
	}
	row := rows[0]

	if row.Amount.Currency() != "EUR" {
		t.Errorf("row.Amount.Currency() = %q, want EUR (from the Currency column, not the USD fallback)", row.Amount.Currency())
	}
	if row.ExternalID != "ext-123" {
		t.Errorf("row.ExternalID = %q, want ext-123", row.ExternalID)
	}
	if row.CategoryHint != "Dining" {
		t.Errorf("row.CategoryHint = %q, want Dining", row.CategoryHint)
	}
	if row.PostedDate == nil {
		t.Fatal("row.PostedDate = nil, want 2026-08-02")
	}
	if row.PostedDate.Year() != 2026 || row.PostedDate.Month() != time.August || row.PostedDate.Day() != 2 {
		t.Errorf("row.PostedDate = %s, want 2026-08-02", row.PostedDate)
	}
}

func TestCSVParser_Parse_FallsBackToDefaultCurrencyWhenCellBlank(t *testing.T) {
	t.Parallel()
	p, err := importparse.NewCSVParser(fullMapping(), "USD", testClock, "UTC")
	if err != nil {
		t.Fatalf("NewCSVParser: %v", err)
	}

	data := "Date,Description,Amount,Posted,Currency,Ref,Category\n" +
		"2026-08-01,Coffee Shop,-4.50,,,,\n"
	rows, err := p.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	row := rows[0]
	if row.Amount.Currency() != "USD" {
		t.Errorf("row.Amount.Currency() = %q, want the USD fallback since the Currency cell is blank", row.Amount.Currency())
	}
	if row.ExternalID != "" {
		t.Errorf("row.ExternalID = %q, want empty", row.ExternalID)
	}
	if row.PostedDate != nil {
		t.Errorf("row.PostedDate = %s, want nil (blank cell)", row.PostedDate)
	}
}

func TestCSVParser_Parse_HeaderMatchIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	p, err := importparse.NewCSVParser(mapping(), "USD", testClock, "UTC")
	if err != nil {
		t.Fatalf("NewCSVParser: %v", err)
	}

	data := "DATE,description,AMOUNT\n2026-08-01,Coffee Shop,-4.50\n"
	rows, err := p.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Parse() returned %d rows, want 1", len(rows))
	}
}

func TestCSVParser_Parse_QuotedFieldWithEmbeddedComma(t *testing.T) {
	t.Parallel()
	p, err := importparse.NewCSVParser(mapping(), "USD", testClock, "UTC")
	if err != nil {
		t.Fatalf("NewCSVParser: %v", err)
	}

	data := "Date,Description,Amount\n2026-08-01,\"Acme, Inc.\",-4.50\n"
	rows, err := p.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if rows[0].Description != "Acme, Inc." {
		t.Errorf("Description = %q, want %q", rows[0].Description, "Acme, Inc.")
	}
}

func TestCSVParser_Parse_Errors(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		data string
	}{
		{"empty file", ""},
		{"header only, no data rows", "Date,Description,Amount\n"},
		{"missing column in header", "Date,Description\n2026-08-01,Coffee,-4.50\n"},
		{"blank date cell", "Date,Description,Amount\n,Coffee Shop,-4.50\n"},
		{"blank description cell", "Date,Description,Amount\n2026-08-01,,-4.50\n"},
		{"blank amount cell", "Date,Description,Amount\n2026-08-01,Coffee Shop,\n"},
		{"unparseable date", "Date,Description,Amount\nnot-a-date,Coffee Shop,-4.50\n"},
		{"unparseable amount", "Date,Description,Amount\n2026-08-01,Coffee Shop,not-a-number\n"},
		{"malformed csv quoting", "Date,Description,Amount\n2026-08-01,\"unterminated,-4.50\n"},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := importparse.NewCSVParser(mapping(), "USD", testClock, "UTC")
			if err != nil {
				t.Fatalf("NewCSVParser: %v", err)
			}
			_, err = p.Parse([]byte(tt.data))
			wantErrCode(t, err, errs.InvalidInput)
		})
	}
}

func TestCSVParser_Parse_RaggedRowTreatsMissingCellsAsBlank(t *testing.T) {
	t.Parallel()
	p, err := importparse.NewCSVParser(fullMapping(), "USD", testClock, "UTC")
	if err != nil {
		t.Fatalf("NewCSVParser: %v", err)
	}

	// This row has no trailing columns at all (no Posted/Currency/Ref/
	// Category cells), which is shorter than the header — FieldsPerRecord
	// is disabled specifically so this doesn't fail the whole file.
	data := "Date,Description,Amount,Posted,Currency,Ref,Category\n2026-08-01,Coffee Shop,-4.50\n"
	rows, err := p.Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("Parse() returned %d rows, want 1", len(rows))
	}
	if rows[0].ExternalID != "" || rows[0].CategoryHint != "" || rows[0].PostedDate != nil {
		t.Errorf("row = %+v, want every unmapped-cell field left blank/nil", rows[0])
	}
}
