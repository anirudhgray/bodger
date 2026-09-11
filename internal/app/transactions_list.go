package app

import (
	"context"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

const (
	// defaultTransactionListLimit is ListTransactions' page size when the
	// caller doesn't specify one. This default lives here, not in
	// ports.TransactionFilter — that port's Limit <= 0 means "no limit" at
	// the low level (AccountBalances relies on exactly that to fetch every
	// matching transaction), so the "sensible page size" default has to be
	// applied above the port, once, here.
	defaultTransactionListLimit = 50
	// maxTransactionListLimit caps how large a single page can be, so a
	// caller can't accidentally (or deliberately) ask for an unbounded
	// result set through the paginated use case — AccountBalances remains
	// the correct way to get "everything".
	maxTransactionListLimit = 200
)

// GetTransactionQuery fetches a single transaction (with its tags) by ID,
// scoped to the actor (ADR-0006). EditTransaction and DeleteTransaction
// already call TransactionRepository.Get directly for their own purposes;
// this is the same lookup exposed as its own use case, for the REST API's
// GET /api/v1/transactions/{id} (issue #8).
type GetTransactionQuery struct {
	ActorID        string
	TransactionRef string
}

// GetTransaction implements the "fetch one" use case
// GET /api/v1/transactions/{id} needs.
func (s *Service) GetTransaction(ctx context.Context, q GetTransactionQuery) (TransactionResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return TransactionResult{}, err
	}
	if strings.TrimSpace(q.TransactionRef) == "" {
		return TransactionResult{}, errs.New(errs.InvalidInput).Explain("A transaction ID is required.").Field("transaction_ref")
	}
	txn, tags, err := s.Transactions.Get(ctx, q.ActorID, q.TransactionRef)
	if err != nil {
		return TransactionResult{}, err
	}
	return TransactionResult{Transaction: txn, Tags: tags}, nil
}

// ListTransactionsQuery is ADR-0009's full TransactionFilter, minus
// ImportBatchRefs (no import_batch concept exists yet — M6) and cursor
// pagination (deferred until a scrolling UI needs it): date range,
// account, category (subtree included by default — ADR-0009, not
// optional), kind, currencies, amount range, description search, and
// tags, offset-paginated with a fully-specified sort.
type ListTransactionsQuery struct {
	ActorID     string
	AccountRef  string
	CategoryRef string
	Kind        string
	DateFrom    string
	DateTo      string
	Currencies  []string
	AmountMin   string
	AmountMax   string
	Description string
	Tags        []string
	TagMode     string
	Limit       int
	Offset      int
}

// ListTransactionsResult is ListTransactions' result: every matching
// transaction, sorted (booked_date DESC, created_at DESC, id DESC) —
// ADR-0009's fully-specified sort, so the same query run twice returns the
// same rows in the same order, including when booked_date and created_at
// tie. Limit and Offset are the values actually applied — after
// defaulting an unset or negative Limit and clamping an oversized one —
// so a caller that builds its own pagination (a REST surface's opaque
// cursor, say) can tell whether a full page came back without needing to
// know this package's default or maximum page size itself.
type ListTransactionsResult struct {
	Transactions []ledger.Transaction
	Limit        int
	Offset       int
}

// ListTransactions implements issue #6's ListTransactions use case.
// DateFrom and DateTo both resolve through normalize.DateOf, so they're
// interpreted in the actor's configured timezone — ADR-0005 and
// data-model.md §9's "the same query returns identical results under
// TZ=UTC and TZ=Asia/Kolkata" applies here as much as it does to a
// transaction's own booked date.
func (s *Service) ListTransactions(ctx context.Context, q ListTransactionsQuery) (ListTransactionsResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ListTransactionsResult{}, err
	}

	var filter ports.TransactionFilter

	if strings.TrimSpace(q.AccountRef) != "" {
		account, err := s.resolveOwnedAccount(ctx, q.ActorID, q.AccountRef)
		if err != nil {
			return ListTransactionsResult{}, attachField(err, "account_ref")
		}
		filter.AccountID = account.ID()
	}

	if strings.TrimSpace(q.CategoryRef) != "" {
		category, err := s.resolveOwnedCategory(ctx, q.ActorID, q.CategoryRef)
		if err != nil {
			return ListTransactionsResult{}, attachField(err, "category_ref")
		}
		filter.CategoryID = category.ID()
	}

	if strings.TrimSpace(q.Kind) != "" {
		kind, err := parseTransactionKind(q.Kind)
		if err != nil {
			return ListTransactionsResult{}, err
		}
		filter.Kind = kind
	}

	if strings.TrimSpace(q.DateFrom) != "" {
		d, err := normalize.DateOf(q.DateFrom, s.Clock, s.Config.UserTimezone)
		if err != nil {
			return ListTransactionsResult{}, err
		}
		filter.FromDate = &d
	}
	if strings.TrimSpace(q.DateTo) != "" {
		d, err := normalize.DateOf(q.DateTo, s.Clock, s.Config.UserTimezone)
		if err != nil {
			return ListTransactionsResult{}, err
		}
		filter.ToDate = &d
	}

	if len(q.Currencies) > 0 {
		currencies, err := validateCurrencyCodes(q.Currencies)
		if err != nil {
			return ListTransactionsResult{}, err
		}
		filter.Currencies = currencies
	}

	amountMin, amountMax, err := validateAmountRange(q.AmountMin, q.AmountMax)
	if err != nil {
		return ListTransactionsResult{}, err
	}
	filter.AmountMin, filter.AmountMax = amountMin, amountMax

	filter.Description = strings.TrimSpace(q.Description)

	if len(q.Tags) > 0 {
		tags := make([]string, 0, len(q.Tags))
		for _, raw := range q.Tags {
			tag, err := normalize.Tag(raw)
			if err != nil {
				return ListTransactionsResult{}, err
			}
			tags = append(tags, tag.String())
		}
		filter.Tags = tags

		tagMode, err := parseTagMode(q.TagMode)
		if err != nil {
			return ListTransactionsResult{}, err
		}
		filter.TagMode = tagMode
	}

	filter.Limit = q.Limit
	if filter.Limit <= 0 {
		filter.Limit = defaultTransactionListLimit
	}
	if filter.Limit > maxTransactionListLimit {
		filter.Limit = maxTransactionListLimit
	}
	filter.Offset = q.Offset
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	txns, err := s.Transactions.List(ctx, q.ActorID, filter)
	if err != nil {
		return ListTransactionsResult{}, err
	}
	return ListTransactionsResult{Transactions: txns, Limit: filter.Limit, Offset: filter.Offset}, nil
}

// validateCurrencyCodes resolves each raw code against money's reference
// data, the same check normalize.Currency makes for a single code — this
// validates a whole filter dimension's worth at once, since
// ports.TransactionFilter.Currencies is OR-within-dimension (ADR-0009),
// not a resolution ladder.
func validateCurrencyCodes(raw []string) ([]string, error) {
	codes := make([]string, 0, len(raw))
	for _, code := range raw {
		code = strings.TrimSpace(code)
		if _, ok := money.LookupCurrency(code); !ok {
			return nil, errs.New(errs.InvalidInput).
				Explain("%q is not a known currency", code).
				Field("currencies")
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// validateAmountRange checks that minRaw/maxRaw (when set) are valid,
// non-negative decimal amounts with minRaw <= maxRaw, and returns them
// unchanged (still raw strings — the SQLite adapter does the actual
// per-currency minor-unit conversion, since ADR-0009's AmountMin/AmountMax
// aren't tied to one currency). Comparison is on absolute value
// (ADR-0009), so a negative bound could never match anything and is
// rejected here rather than silently accepted.
func validateAmountRange(minRaw, maxRaw string) (string, string, error) {
	var minDec, maxDec decimal.Decimal
	var hasMin, hasMax bool

	if strings.TrimSpace(minRaw) != "" {
		d, err := decimal.NewFromString(strings.TrimSpace(minRaw))
		if err != nil || d.IsNegative() {
			return "", "", errs.New(errs.InvalidInput).
				Explain("%q isn't a valid amount", minRaw).
				Field("amount_min")
		}
		minDec, hasMin = d, true
	}
	if strings.TrimSpace(maxRaw) != "" {
		d, err := decimal.NewFromString(strings.TrimSpace(maxRaw))
		if err != nil || d.IsNegative() {
			return "", "", errs.New(errs.InvalidInput).
				Explain("%q isn't a valid amount", maxRaw).
				Field("amount_max")
		}
		maxDec, hasMax = d, true
	}
	if hasMin && hasMax && minDec.GreaterThan(maxDec) {
		return "", "", errs.New(errs.InvalidInput).
			Explain("amount_min (%s) is greater than amount_max (%s)", minRaw, maxRaw).
			Field("amount_min")
	}

	result := func(raw string, has bool) string {
		if !has {
			return ""
		}
		return strings.TrimSpace(raw)
	}
	return result(minRaw, hasMin), result(maxRaw, hasMax), nil
}

// parseTagMode validates raw against ADR-0009's TagMode enum, defaulting
// an unset mode to "any" — the general "multiple values within one
// dimension are OR" rule (ADR-0009), applied here because it only matters
// once ListTransactions already knows Tags is non-empty.
func parseTagMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "":
		return "any", nil
	case "any", "all":
		return mode, nil
	default:
		return "", errs.New(errs.InvalidInput).
			Explain("%q isn't a valid tag mode.", raw).
			Field("tag_mode").
			With("valid_modes", []string{"any", "all"})
	}
}
