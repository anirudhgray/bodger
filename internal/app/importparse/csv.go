package importparse

import (
	"encoding/csv"
	"errors"
	"io"
	"strings"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// ColumnMapping names the CSV header columns that hold each of a Row's
// fields — ADR-0008's "column mapping" stage, resolved once per import
// rather than reimplemented per source file. Column names are matched
// against the file's own header row case-insensitively (leading/trailing
// space ignored). An empty value in PostedDateColumn, CurrencyColumn,
// ExternalIDColumn, or CategoryColumn means the source has no such
// column, not that it's blank for every row — those four are optional.
type ColumnMapping struct {
	// DateColumn, DescriptionColumn, and AmountColumn are required: every
	// bank or statement export has some notion of each.
	DateColumn        string
	DescriptionColumn string
	AmountColumn      string
	// PostedDateColumn, CurrencyColumn, ExternalIDColumn, and
	// CategoryColumn are optional. Leave empty when the source has no such
	// column.
	PostedDateColumn string
	CurrencyColumn   string
	ExternalIDColumn string
	CategoryColumn   string
}

// CSVParser is the first (and, for now, only) Parser implementation: a
// plain CSV file with a header row and one record per data row
// (ADR-0008). Multi-line quoted fields are supported (encoding/csv handles
// them); a source that splits its amount across separate debit/credit
// columns instead of one signed column isn't — pre-process such a file
// into a single signed amount column, or extend ColumnMapping when a real
// source needs that shape (a new field here is a small, explicit addition,
// never a guess at how to combine two ambiguous columns).
type CSVParser struct {
	mapping          ColumnMapping
	fallbackCurrency string
	clock            clock.Clock
	timezone         string
}

var _ Parser = CSVParser{}

// NewCSVParser constructs a CSVParser. fallbackCurrency is the currency
// used for a row whose currency cell is blank (or whose mapping has no
// CurrencyColumn at all) — normally the target account's own currency;
// resolving *which* account that is happens one layer up, in
// internal/app, since this package has no account repository and no
// opinion on accounts at all (ADR-0008).
//
// clk and tz are threaded through to normalize.DateOf exactly as every
// other date-parsing call site in the application layer does (ADR-0005).
// A CSV's own dates are almost always unambiguous (ISO, or a configured
// locale format), but DateOf still needs a timezone to interpret them
// against — reusing it, rather than parsing dates a second, CSV-specific
// way, is this package's entire reason for living under internal/app
// rather than internal/domain or a leaf utility package.
func NewCSVParser(mapping ColumnMapping, fallbackCurrency string, clk clock.Clock, tz string) (CSVParser, error) {
	if strings.TrimSpace(mapping.DateColumn) == "" {
		return CSVParser{}, errs.New(errs.InvalidInput).Explain("A date column is required.").Field("date_column")
	}
	if strings.TrimSpace(mapping.DescriptionColumn) == "" {
		return CSVParser{}, errs.New(errs.InvalidInput).Explain("A description column is required.").Field("description_column")
	}
	if strings.TrimSpace(mapping.AmountColumn) == "" {
		return CSVParser{}, errs.New(errs.InvalidInput).Explain("An amount column is required.").Field("amount_column")
	}
	if _, ok := money.LookupCurrency(fallbackCurrency); !ok {
		return CSVParser{}, errs.New(errs.InvalidInput).Explain("%q is not a known currency.", fallbackCurrency).Field("currency")
	}
	return CSVParser{mapping: mapping, fallbackCurrency: fallbackCurrency, clock: clk, timezone: tz}, nil
}

// Parse implements Parser: it reads data as a CSV file whose first row is
// a header, matches it against the configured ColumnMapping, and returns
// one Row per data row in file order.
func (p CSVParser) Parse(data []byte) ([]Row, error) {
	reader := csv.NewReader(strings.NewReader(string(data)))
	// Real bank exports are not always perfectly rectangular (a trailing
	// column on some rows, a short row here and there) — -1 disables
	// encoding/csv's default "every record has the same field count as the
	// first" check, and cell() below treats a short row's missing cells as
	// blank rather than failing the whole file over one ragged line.
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, errs.New(errs.InvalidInput).Explain("The file is empty.")
	}
	if err != nil {
		return nil, errs.New(errs.InvalidInput).Explain("The file isn't valid CSV.").Wrap(err)
	}

	cols, err := resolveColumns(header, p.mapping)
	if err != nil {
		return nil, err
	}

	var rows []Row
	sortOrder := 0
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errs.New(errs.InvalidInput).
				Explain("Row %d of the file isn't valid CSV.", rowNumber(sortOrder)).
				Wrap(err)
		}

		row, err := p.buildRow(record, cols, sortOrder)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
		sortOrder++
	}

	if len(rows) == 0 {
		return nil, errs.New(errs.InvalidInput).Explain("The file has no data rows to import.")
	}
	return rows, nil
}

// rowNumber converts a zero-based data-row index into the 1-based line
// number a person looking at the file in a spreadsheet would call it: +1
// for the header row, +1 to go from zero- to one-based.
func rowNumber(sortOrder int) int { return sortOrder + 2 }

// csvColumns is resolveColumns' output: each mapped field's index into a
// data row, with -1 meaning "the mapping doesn't name a column for this
// field".
type csvColumns struct {
	date, description, amount int
	postedDate, currency      int
	externalID, category      int
}

// resolveColumns matches every non-empty ColumnMapping field against
// header (case-insensitively; the first column with a given name wins on
// a duplicate header), and returns an error naming the missing column if
// any mapped name — required or optional — isn't present in the file at
// all. A caller-specified mapping that names a column the file doesn't
// have is a configuration mistake worth surfacing immediately, not
// silently ignored.
func resolveColumns(header []string, mapping ColumnMapping) (csvColumns, error) {
	index := make(map[string]int, len(header))
	for i, h := range header {
		key := strings.ToLower(strings.TrimSpace(h))
		if _, exists := index[key]; !exists {
			index[key] = i
		}
	}

	find := func(name string) (int, error) {
		if strings.TrimSpace(name) == "" {
			return -1, nil
		}
		i, ok := index[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return -1, errs.New(errs.InvalidInput).
				Explain("The file has no column named %q.", name).
				Field("column_mapping")
		}
		return i, nil
	}

	var cols csvColumns
	var err error
	if cols.date, err = find(mapping.DateColumn); err != nil {
		return csvColumns{}, err
	}
	if cols.description, err = find(mapping.DescriptionColumn); err != nil {
		return csvColumns{}, err
	}
	if cols.amount, err = find(mapping.AmountColumn); err != nil {
		return csvColumns{}, err
	}
	if cols.postedDate, err = find(mapping.PostedDateColumn); err != nil {
		return csvColumns{}, err
	}
	if cols.currency, err = find(mapping.CurrencyColumn); err != nil {
		return csvColumns{}, err
	}
	if cols.externalID, err = find(mapping.ExternalIDColumn); err != nil {
		return csvColumns{}, err
	}
	if cols.category, err = find(mapping.CategoryColumn); err != nil {
		return csvColumns{}, err
	}
	return cols, nil
}

// cell returns record[i], trimmed, or "" if i is -1 (the field isn't
// mapped) or out of range (a ragged row shorter than the header).
func cell(record []string, i int) string {
	if i < 0 || i >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[i])
}

// buildRow turns one already-split CSV record into a Row. This is the one
// place a CSV cell's text becomes a domain value, and it does that
// entirely through internal/app/normalize — the same functions every other
// surface calls (ADR-0005) — never a parallel CSV-specific parse.
func (p CSVParser) buildRow(record []string, cols csvColumns, sortOrder int) (Row, error) {
	rowNum := rowNumber(sortOrder)

	dateCell := cell(record, cols.date)
	if dateCell == "" {
		return Row{}, errs.New(errs.InvalidInput).Explain("Row %d has no date.", rowNum).Field("date")
	}
	// dateCell is known non-empty, so normalize.DateOf's own
	// empty-means-today convention (correct for a command field a user
	// left blank on purpose) never applies here — a blank date cell is
	// caught above instead, since a source row silently defaulting to
	// today's date would be a wrong booked date with no signal.
	bookedDate, err := normalize.DateOf(dateCell, p.clock, p.timezone)
	if err != nil {
		return Row{}, attachRow(err, rowNum)
	}

	description, err := normalize.Text(cell(record, cols.description), 0)
	if err != nil {
		return Row{}, attachRow(err, rowNum)
	}
	if description == "" {
		return Row{}, errs.New(errs.InvalidInput).Explain("Row %d has no description.", rowNum).Field("description")
	}

	amountCell := cell(record, cols.amount)
	if amountCell == "" {
		return Row{}, errs.New(errs.InvalidInput).Explain("Row %d has no amount.", rowNum).Field("amount")
	}
	currency, err := normalize.Currency(cell(record, cols.currency), "", "", p.fallbackCurrency)
	if err != nil {
		return Row{}, attachRow(err, rowNum)
	}
	amountMinor, err := normalize.Amount(amountCell, currency)
	if err != nil {
		return Row{}, attachRow(err, rowNum)
	}
	amount, err := money.NewMoney(amountMinor, currency)
	if err != nil {
		return Row{}, errs.New(errs.Internal).Wrap(err)
	}

	row := Row{
		RawPayload:   strings.Join(record, ","),
		BookedDate:   bookedDate,
		Description:  description,
		Amount:       amount,
		ExternalID:   cell(record, cols.externalID),
		CategoryHint: cell(record, cols.category),
		SortOrder:    sortOrder,
	}

	if postedCell := cell(record, cols.postedDate); postedCell != "" {
		postedDate, err := normalize.DateOf(postedCell, p.clock, p.timezone)
		if err != nil {
			return Row{}, attachRow(err, rowNum)
		}
		row.PostedDate = &postedDate
	}

	return row, nil
}

// attachRow prefixes err's message with which file row it came from, the
// same best-effort pattern internal/app's own attachField uses for a
// normalize error that doesn't know the context it's being resolved in.
func attachRow(err error, rowNum int) error {
	var e *errs.Error
	if errors.As(err, &e) {
		_ = e.Explain("Row %d: %s", rowNum, e.Message)
	}
	return err
}
