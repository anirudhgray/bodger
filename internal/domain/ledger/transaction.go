package ledger

import (
	"fmt"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

// TransactionKind is one of the three shapes data-model.md §5 documents.
// It is denormalised against the postings' actual shape by design (ADR-0003
// "Consequences") — but disagreement between the two is structurally
// impossible here, because NewOutflow, NewInflow, and NewTransfer are the
// only ways to produce a Transaction. Each sets its own kind and validates
// its own shape; there is no generic constructor that takes an arbitrary
// kind alongside an arbitrary slice of postings.
type TransactionKind string

const (
	TransactionKindOutflow  TransactionKind = "outflow"
	TransactionKindInflow   TransactionKind = "inflow"
	TransactionKindTransfer TransactionKind = "transfer"
)

// Transaction is one thing that happened, on one date, with one or more
// postings. Its fields are unexported: the only way to produce one is
// NewOutflow, NewInflow, or NewTransfer.
type Transaction struct {
	id                   string
	userID               string
	kind                 TransactionKind
	bookedDate           domain.Date
	postedDate           *domain.Date
	description          string
	notes                string
	deletedAt            *time.Time
	importRecordID       *string
	externalID           *string
	relatedTransactionID *string
	postings             []Posting
	fxRate               *fx.Rate
	fxRateSource         string
}

// FxRateSourceImplied is the fx_rate_source value ADR-0004 specifies for a
// cross-currency transfer's derived rate ("Cross-currency transfers record
// both legs") — the only source WithFxRate is used with in practice, since
// there is no manual-entry path (migration 00011_add_fx_rates.sql's doc
// comment).
const FxRateSourceImplied = "implied"

// TransactionOption sets one of a Transaction's optional fields at
// construction time. See WithNotes, WithPostedDate,
// WithRelatedTransaction, and WithImportProvenance.
type TransactionOption func(*Transaction)

// WithNotes sets the transaction's free-text notes.
func WithNotes(notes string) TransactionOption {
	return func(t *Transaction) { t.notes = notes }
}

// WithPostedDate sets the date an import's source reported, distinct from
// booked_date and never used for reporting — data-model.md §9.
func WithPostedDate(d domain.Date) TransactionOption {
	return func(t *Transaction) {
		posted := d
		t.postedDate = &posted
	}
}

// WithRelatedTransaction points this transaction at another one it
// refunds, reverses, or corrects. A refund is an ordinary inflow that uses
// this option and the original outflow's category — data-model.md §7.
func WithRelatedTransaction(id string) TransactionOption {
	return func(t *Transaction) {
		related := id
		t.relatedTransactionID = &related
	}
}

// WithImportProvenance records where an imported transaction came from.
// Behaviour around these fields (deduplication, batches) is out of this
// package's scope (issue #5) — this option only carries the values.
func WithImportProvenance(importRecordID, externalID string) TransactionOption {
	return func(t *Transaction) {
		if importRecordID != "" {
			irid := importRecordID
			t.importRecordID = &irid
		}
		if externalID != "" {
			eid := externalID
			t.externalID = &eid
		}
	}
}

func newTransaction(id, userID string, kind TransactionKind, bookedDate domain.Date, description string, postings []Posting, opts []TransactionOption) (Transaction, error) {
	if id == "" {
		return Transaction{}, ErrTransactionEmptyID
	}
	if userID == "" {
		return Transaction{}, ErrTransactionEmptyUserID
	}
	if description == "" {
		return Transaction{}, ErrTransactionEmptyDescription
	}
	if len(postings) == 0 {
		return Transaction{}, ErrTransactionNoPostings
	}

	cp := make([]Posting, len(postings))
	copy(cp, postings)

	t := Transaction{
		id:          id,
		userID:      userID,
		kind:        kind,
		bookedDate:  bookedDate,
		description: description,
		postings:    cp,
	}
	for _, opt := range opts {
		opt(&t)
	}
	return t, nil
}

// NewOutflow constructs an outflow transaction: kind=outflow, every
// posting negative and on the same account (data-model.md §14). A split is
// simply an outflow with more than one posting; its total is Total() —
// computed from the postings, never stored.
func NewOutflow(id, userID string, bookedDate domain.Date, description string, postings []Posting, opts ...TransactionOption) (Transaction, error) {
	t, err := newTransaction(id, userID, TransactionKindOutflow, bookedDate, description, postings, opts)
	if err != nil {
		return Transaction{}, err
	}
	if err := validateSingleAccountPostings(t.postings, false); err != nil {
		return Transaction{}, err
	}
	return t, nil
}

// NewInflow constructs an inflow transaction: kind=inflow, every posting
// positive and on the same account.
func NewInflow(id, userID string, bookedDate domain.Date, description string, postings []Posting, opts ...TransactionOption) (Transaction, error) {
	t, err := newTransaction(id, userID, TransactionKindInflow, bookedDate, description, postings, opts)
	if err != nil {
		return Transaction{}, err
	}
	if err := validateSingleAccountPostings(t.postings, true); err != nil {
		return Transaction{}, err
	}
	return t, nil
}

// validateSingleAccountPostings enforces the shape shared by outflow and
// inflow: every posting on the same account, and every amount the
// required sign. A zero amount satisfies neither "positive" nor
// "negative", so it's rejected here without a separate check.
func validateSingleAccountPostings(postings []Posting, wantPositive bool) error {
	account := postings[0].AccountID()
	for _, p := range postings {
		if p.AccountID() != account {
			return fmt.Errorf("%w: %q and %q", ErrPostingsMultipleAccounts, account, p.AccountID())
		}
		if wantPositive {
			if p.Amount().IsNegative() || p.Amount().IsZero() {
				return fmt.Errorf("%w: %s", ErrInflowPostingNotPositive, p.Amount())
			}
		} else if !p.Amount().IsNegative() {
			return fmt.Errorf("%w: %s", ErrOutflowPostingNotNegative, p.Amount())
		}
	}
	return nil
}

// NewTransfer constructs a transfer transaction: kind=transfer, exactly
// two postings on two distinct accounts, opposite signs, and both
// categories null (data-model.md §14).
//
// Same-currency and cross-currency transfers both succeed, but the
// zero-sum rule applies to only the former: a same-currency transfer's two
// postings must sum to exactly zero, while a cross-currency transfer's
// zero-sum rule is suspended entirely (data-model.md §3/§5, ADR-0004
// "Cross-currency transfers record both legs") — both legs are
// independently authoritative facts the user knows, and the exchange rate
// between them is the *derived* quantity, not the other way round.
//
// NewTransfer returns the transfer's implied fx.Rate alongside the
// Transaction rather than as a side channel, because every transfer has
// one: a same-currency transfer's rate is the trivial 1:1
// (fx.IdentityRate), and a cross-currency transfer's rate is derived from
// the two leg amounts (fx.DeriveImpliedRate). Returning a uniform Rate for
// both cases means a caller that persists fx_rate_used/fx_rate_source
// (app-layer work, issue #133) never has to special-case "was there a rate
// or not" — there always is one, trivial or not.
func NewTransfer(id, userID string, bookedDate domain.Date, description string, postings []Posting, opts ...TransactionOption) (Transaction, fx.Rate, error) {
	t, err := newTransaction(id, userID, TransactionKindTransfer, bookedDate, description, postings, opts)
	if err != nil {
		return Transaction{}, fx.Rate{}, err
	}
	if len(t.postings) != 2 {
		return Transaction{}, fx.Rate{}, fmt.Errorf("%w: got %d", ErrTransferPostingCount, len(t.postings))
	}
	a, b := t.postings[0], t.postings[1]

	if a.AccountID() == b.AccountID() {
		return Transaction{}, fx.Rate{}, fmt.Errorf("%w: %q", ErrTransferSameAccount, a.AccountID())
	}
	if _, ok := a.CategoryID(); ok {
		return Transaction{}, fx.Rate{}, ErrTransferPostingHasCategory
	}
	if _, ok := b.CategoryID(); ok {
		return Transaction{}, fx.Rate{}, ErrTransferPostingHasCategory
	}
	if !oppositeSigns(a.Amount(), b.Amount()) {
		return Transaction{}, fx.Rate{}, ErrTransferPostingsMustOppose
	}

	if a.Currency() == b.Currency() {
		sum, err := money.Sum(a.Amount(), b.Amount())
		if err != nil {
			return Transaction{}, fx.Rate{}, err
		}
		if !sum.IsZero() {
			return Transaction{}, fx.Rate{}, fmt.Errorf("%w: %s", ErrTransferNotBalanced, sum)
		}
		rate, err := fx.IdentityRate(a.Currency())
		if err != nil {
			return Transaction{}, fx.Rate{}, err
		}
		return t, rate, nil
	}

	// Cross-currency: the zero-sum rule above is deliberately skipped.
	// base is the outflow leg (the money that left an account) and quote
	// is the inflow leg (the money that arrived) — see
	// fx.DeriveImpliedRate's doc comment for why that ordering matches
	// ADR-0004's worked example.
	base, quote := b.Amount(), a.Amount()
	if a.Amount().IsNegative() {
		base, quote = a.Amount(), b.Amount()
	}
	rate, err := fx.DeriveImpliedRate(base, quote)
	if err != nil {
		return Transaction{}, fx.Rate{}, err
	}
	return t, rate, nil
}

// oppositeSigns reports whether a and b are one strictly negative and one
// strictly positive. Either being zero is never "opposite" — a transfer
// posting with a zero amount didn't move any money.
func oppositeSigns(a, b money.Money) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	return a.IsNegative() != b.IsNegative()
}

// ID returns the transaction's identifier.
func (t Transaction) ID() string { return t.id }

// UserID returns the ID of the user who owns the transaction.
func (t Transaction) UserID() string { return t.userID }

// Kind returns the transaction's kind.
func (t Transaction) Kind() TransactionKind { return t.kind }

// BookedDate returns the transaction's reporting date.
func (t Transaction) BookedDate() domain.Date { return t.bookedDate }

// Description returns the transaction's description.
func (t Transaction) Description() string { return t.description }

// Notes returns the transaction's free-text notes, empty if none were set.
func (t Transaction) Notes() string { return t.notes }

// PostedDate returns the date an import's source reported, and false if
// none was set.
func (t Transaction) PostedDate() (domain.Date, bool) {
	if t.postedDate == nil {
		return domain.Date{}, false
	}
	return *t.postedDate, true
}

// ImportRecordID returns the import record this transaction came from, and
// false if it wasn't imported.
func (t Transaction) ImportRecordID() (string, bool) {
	if t.importRecordID == nil {
		return "", false
	}
	return *t.importRecordID, true
}

// ExternalID returns the source system's ID for this transaction, and
// false if it has none.
func (t Transaction) ExternalID() (string, bool) {
	if t.externalID == nil {
		return "", false
	}
	return *t.externalID, true
}

// RelatedTransactionID returns the transaction this one refunds, reverses,
// or corrects, and false if it has none.
func (t Transaction) RelatedTransactionID() (string, bool) {
	if t.relatedTransactionID == nil {
		return "", false
	}
	return *t.relatedTransactionID, true
}

// Postings returns a copy of the transaction's postings — mutating the
// returned slice never affects t.
func (t Transaction) Postings() []Posting {
	cp := make([]Posting, len(t.postings))
	copy(cp, t.postings)
	return cp
}

// Total sums the transaction's postings. There is nothing else to disagree
// with this — data-model.md §5: "no separate total is stored, because a
// stored total is a second source of truth waiting to disagree."
func (t Transaction) Total() (money.Money, error) {
	amounts := make([]money.Money, len(t.postings))
	for i, p := range t.postings {
		amounts[i] = p.Amount()
	}
	return money.Sum(amounts...)
}

// Delete returns a copy of t with deletedAt set to at; t itself is
// unchanged. ADR-0002: deletion is a soft delete, and this package holds
// no clock, so at is supplied by the caller (the app layer's injected
// clock).
func (t Transaction) Delete(at time.Time) Transaction {
	deleted := at
	t.deletedAt = &deleted
	return t
}

// IsDeleted reports whether the transaction has been soft-deleted.
func (t Transaction) IsDeleted() bool { return t.deletedAt != nil }

// DeletedAt returns when the transaction was soft-deleted, and false if it
// hasn't been.
func (t Transaction) DeletedAt() (time.Time, bool) {
	if t.deletedAt == nil {
		return time.Time{}, false
	}
	return *t.deletedAt, true
}

// FxRate returns the transaction's persisted exchange rate and its source,
// and false if none was attached. Only a cross-currency transfer ever has
// one — NewTransfer computes a same-currency transfer's trivial 1:1 rate
// too, but nothing attaches it here, per ADR-0004's "the implied rate ...
// is derived and stored on the transaction" applying to the cross-currency
// case alone.
func (t Transaction) FxRate() (fx.Rate, string, bool) {
	if t.fxRate == nil {
		return fx.Rate{}, "", false
	}
	return *t.fxRate, t.fxRateSource, true
}

// WithFxRate returns a copy of t carrying rate as its persisted exchange
// rate, attributed to source; t itself is unchanged. This is a
// post-construction copy-transform in the same shape as Delete, not a
// TransactionOption: both RecordTransfer/EditTransaction (issue #133) and
// the sqlite adapter's read path only have a rate to attach *after* they
// already hold a Transaction in hand — the former because NewTransfer
// computes and returns the rate itself rather than accepting one as an
// option, and the latter because it is restoring an already-computed value
// straight from storage, not re-deriving it from postings.
func (t Transaction) WithFxRate(rate fx.Rate, source string) Transaction {
	r := rate
	t.fxRate = &r
	t.fxRateSource = source
	return t
}
