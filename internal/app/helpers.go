package app

import (
	"context"
	"strings"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/clock"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// requireActorID is the one check every use-case method starts with
// (ADR-0006: "every command and query struct carries an ActorID"). It
// exists here, in internal/app, rather than being borrowed from
// internal/adapters/sqlite's identical-looking requireActor: the app layer
// makes its own authorisation decisions and doesn't depend on how a
// particular adapter happens to validate its own inputs.
func requireActorID(actorID string) error {
	if strings.TrimSpace(actorID) == "" {
		return errs.New(errs.InvalidInput).Explain("An actor is required.").Field("actor_id")
	}
	return nil
}

// optionalDate resolves raw into a *domain.Date, treating an empty (or
// whitespace-only) string as "not supplied" -> nil, rather than delegating
// to normalize.DateOf's own empty-means-today convention. This is a
// deliberate difference from a transaction's booked date: an account's
// opening-balance date is a genuinely optional fact ("an account with no
// declared opening-balance date simply starts its history at its first
// transaction" - data-model.md §4), and defaulting a field the caller
// simply didn't set to today would silently invent a fact nobody asserted.
// A caller that means "today" says so explicitly.
func optionalDate(raw string, clk clock.Clock, tz string) (*domain.Date, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	d, err := normalize.DateOf(raw, clk, tz)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// optionalText normalises raw via normalize.Text, treating an empty (or
// whitespace-only, or normalises-to-empty) result as "not supplied" -> nil
// rather than a pointer to "". field is attached to any error normalize.Text
// returns, the same way Text's own doc comment describes callers doing.
func optionalText(raw string, maxLen int, field string) (*string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	v, err := normalize.Text(raw, maxLen)
	if err != nil {
		return nil, attachField(err, field)
	}
	if v == "" {
		return nil, nil
	}
	return &v, nil
}

// normalizeTags normalises every raw tag via normalize.Tag, deduplicating
// (case- and normalisation-insensitive, since normalize.Tag already folds
// case) while preserving first-seen order. An empty raw slice returns an
// empty, non-nil slice's zero value (nil) is fine too — callers pass this
// straight to ports.TransactionRepository.Create/Update.
func normalizeTags(raw []string) ([]ledger.Tag, error) {
	tags := make([]ledger.Tag, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, r := range raw {
		t, err := normalize.Tag(r)
		if err != nil {
			return nil, err
		}
		if seen[t.String()] {
			continue
		}
		seen[t.String()] = true
		tags = append(tags, t)
	}
	return tags, nil
}

// signedAmount returns the magnitude of minor with the sign wantPositive
// requires — the shared "an outflow amount is always negative, an inflow
// amount is always positive, regardless of how the user typed it" rule
// RecordOutflow, RecordInflow, and EditTransaction all need identically.
func signedAmount(minor int64, wantPositive bool) int64 {
	abs := minor
	if abs < 0 {
		abs = -abs
	}
	if wantPositive {
		return abs
	}
	return -abs
}

// accountCandidates adapts accounts into the normalize.Candidate list
// normalize.Ref resolves an AccountRef command field against. The
// candidates always come from a List call already scoped to the acting
// user (ports.AccountRepository.List's contract - "never returns another
// user's accounts"), which is what makes resolving a ref through this
// function double as the app layer's own authorisation decision
// (ADR-0006): a ref can never resolve to an ID that isn't already the
// actor's own.
func accountCandidates(accounts []ledger.Account) []normalize.Candidate {
	out := make([]normalize.Candidate, len(accounts))
	for i, a := range accounts {
		out[i] = normalize.Candidate{ID: a.ID(), Name: a.Name()}
	}
	return out
}

// categoryCandidates is categoryCandidates' account.go equivalent for
// categories - see accountCandidates for the authorisation rationale.
func categoryCandidates(categories []ledger.Category) []normalize.Candidate {
	out := make([]normalize.Candidate, len(categories))
	for i, c := range categories {
		out[i] = normalize.Candidate{ID: c.ID(), Name: c.Name()}
	}
	return out
}

// findAccount returns the account in accounts whose ID is id. It's called
// only after normalize.Ref has already resolved id against this exact
// slice's candidates, so the account is always present; the zero value it
// falls back to is unreachable in practice.
func findAccount(accounts []ledger.Account, id string) ledger.Account {
	for _, a := range accounts {
		if a.ID() == id {
			return a
		}
	}
	return ledger.Account{}
}

// findCategory is findAccount's categories equivalent.
func findCategory(categories []ledger.Category, id string) ledger.Category {
	for _, c := range categories {
		if c.ID() == id {
			return c
		}
	}
	return ledger.Category{}
}

// resolveOwnedAccount resolves ref (a UUID or name) against actorID's own
// accounts and returns the matching Account, or a *errs.Error (NotFound,
// or a disambiguation InvalidInput) if it doesn't resolve. Because the
// candidate list underneath normalize.Ref only ever contains actorID's own
// accounts, a successful resolution is itself the app layer's
// authorisation decision that this account belongs to this actor
// (ADR-0006) - there is no separate ownership check to remember to add.
func (s *Service) resolveOwnedAccount(ctx context.Context, actorID, ref string) (ledger.Account, error) {
	accounts, err := s.Accounts.List(ctx, actorID)
	if err != nil {
		return ledger.Account{}, err
	}
	id, err := normalize.Ref(ref, accountCandidates(accounts))
	if err != nil {
		return ledger.Account{}, err
	}
	return findAccount(accounts, id), nil
}

// resolveOwnedCategory is resolveOwnedAccount's categories equivalent.
func (s *Service) resolveOwnedCategory(ctx context.Context, actorID, ref string) (ledger.Category, error) {
	categories, err := s.Categories.List(ctx, actorID)
	if err != nil {
		return ledger.Category{}, err
	}
	id, err := normalize.Ref(ref, categoryCandidates(categories))
	if err != nil {
		return ledger.Category{}, err
	}
	return findCategory(categories, id), nil
}

// parseAccountKind validates raw against ledger's closed set of account
// kinds. Kind has no I/O or clock dependency (ADR-0005's "only fields with
// no such dependency are typed at the boundary" would let a surface type
// this directly as ledger.AccountKind) - it's kept as a raw string on
// command structs anyway, for the same "one uniform rule, no per-field
// exception" reasoning ADR-0005 gives for Tags: a surface that can't import
// internal/domain at all (the import-graph check) couldn't reference
// ledger.AccountKindBank regardless, so the command field has to be a
// string no matter what, and validating it here rather than trusting a
// surface's own enum keeps exactly one place that knows the valid set.
func parseAccountKind(raw string) (ledger.AccountKind, error) {
	k := ledger.AccountKind(strings.ToLower(strings.TrimSpace(raw)))
	switch k {
	case ledger.AccountKindBank, ledger.AccountKindCash, ledger.AccountKindCreditCard,
		ledger.AccountKindWallet, ledger.AccountKindInvestment, ledger.AccountKindLoan, ledger.AccountKindOther:
		return k, nil
	default:
		return "", errs.New(errs.InvalidInput).
			Explain("%q isn't a valid account type.", raw).
			Field("kind").
			With("valid_kinds", []string{"bank", "cash", "credit_card", "wallet", "investment", "loan", "other"})
	}
}

// parseCategoryKind is parseAccountKind's category-kind equivalent.
func parseCategoryKind(raw string) (ledger.CategoryKind, error) {
	k := ledger.CategoryKind(strings.ToLower(strings.TrimSpace(raw)))
	switch k {
	case ledger.CategoryKindExpense, ledger.CategoryKindIncome:
		return k, nil
	default:
		return "", errs.New(errs.InvalidInput).
			Explain("%q isn't a valid category type.", raw).
			Field("kind").
			With("valid_kinds", []string{"expense", "income"})
	}
}

// parseTransactionKind is parseAccountKind's transaction-kind equivalent,
// used by ListTransactions to validate the optional Kind filter field.
func parseTransactionKind(raw string) (ledger.TransactionKind, error) {
	k := ledger.TransactionKind(strings.ToLower(strings.TrimSpace(raw)))
	switch k {
	case ledger.TransactionKindOutflow, ledger.TransactionKindInflow, ledger.TransactionKindTransfer:
		return k, nil
	default:
		return "", errs.New(errs.InvalidInput).
			Explain("%q isn't a valid transaction type.", raw).
			Field("kind").
			With("valid_kinds", []string{"outflow", "inflow", "transfer"})
	}
}

// accountOpeningBalanceDatePtr, accountInstitutionPtr, and
// accountArchivedAtPtr adapt Account's "(value, bool)" optional accessors
// into the "*T, nil for absent" shape ledger.NewAccount's parameters take,
// so a rename/set-opening-balance/archive use case can pass every
// unchanged optional field straight back into the constructor without
// three lines of if/ok boilerplate at every call site.
func accountOpeningBalanceDatePtr(a ledger.Account) *domain.Date {
	if d, ok := a.OpeningBalanceDate(); ok {
		return &d
	}
	return nil
}

func accountInstitutionPtr(a ledger.Account) *string {
	if v, ok := a.Institution(); ok {
		return &v
	}
	return nil
}

func accountArchivedAtPtr(a ledger.Account) *domain.Date {
	if d, ok := a.ArchivedAt(); ok {
		return &d
	}
	return nil
}

// categoryParentPtr and categoryArchivedAtPtr are accountOpeningBalanceDatePtr's
// category equivalents.
func categoryParentPtr(c ledger.Category) *string {
	if id, ok := c.ParentID(); ok {
		return &id
	}
	return nil
}

func categoryArchivedAtPtr(c ledger.Category) *domain.Date {
	if d, ok := c.ArchivedAt(); ok {
		return &d
	}
	return nil
}
