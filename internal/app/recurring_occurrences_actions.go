package app

import (
	"context"

	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// MaterialiseOccurrenceCommand turns a pending ScheduledOccurrence into a
// real Transaction — data-model.md §11's "occurrence becomes actual"
// boundary. It always uses the occurrence's owning rule's *current* amount,
// account, category, and description, and the occurrence's own projected
// date: an occurrence carries no copy of any of these itself (only id,
// rule_id, occurrence_date, status, transaction_id — ADR-0014's "no
// account, no currency, no Money"), so there is nothing else to build a
// transaction from, and nothing "frozen" to fall out of sync with a later
// rule edit. This is also why materialising an occurrence whose rule has
// since been edited, or even archived, is still valid — an archived rule
// only stops future generation (issue #278), it doesn't invalidate an
// already-generated pending occurrence.
//
// There is deliberately no way to override the amount or date for this one
// materialisation (e.g. the actual bill coming in for a different amount)
// — ADR-0014 doesn't specify this, and issue #289 tracks it as a separate,
// deferred follow-up. Today's path for that case is: materialise with the
// projected fields, then EditTransaction the result.
type MaterialiseOccurrenceCommand struct {
	ActorID      string
	OccurrenceID string
}

// MaterialiseOccurrenceResult wraps both halves of a materialisation: the
// transaction it produced, and the occurrence's own updated state.
type MaterialiseOccurrenceResult struct {
	Transaction ledger.Transaction
	Occurrence  recurring.ScheduledOccurrence
}

// resolveOccurrenceMoney resolves the account, category, and signed
// money.Money value that materialising rule's next firing would use — the
// one and only path through which anything may derive a monetary value for
// one of rule's occurrences (ADR-0014). An occurrence itself carries no
// account, no currency, and no Money; every value here comes from the
// rule's own account, category, and amountMinor, exactly as
// MaterialiseOccurrence's own doc comment describes, and rule.AmountMinor()
// is always strictly positive — the returned money.Money's sign instead
// follows category.Kind() the same way buildOutflowOrInflowPosting's
// wantPositive dispatch does for a user-entered amount.
//
// MaterialiseOccurrence and (issue #301) findOccurrenceMatch both call this
// rather than each computing their own account/category/sign lookup, so
// there is exactly one place that knows how to turn a RecurringRule into a
// concrete, signed amount.
func (s *Service) resolveOccurrenceMoney(ctx context.Context, actorID string, rule recurring.RecurringRule) (ledger.Account, ledger.Category, money.Money, error) {
	account, err := s.resolveOwnedAccount(ctx, actorID, rule.AccountID())
	if err != nil {
		return ledger.Account{}, ledger.Category{}, money.Money{}, err
	}
	category, err := s.resolveOwnedCategory(ctx, actorID, rule.CategoryID())
	if err != nil {
		return ledger.Account{}, ledger.Category{}, money.Money{}, err
	}

	wantPositive := category.Kind() == ledger.CategoryKindIncome
	amountMinor := signedAmount(rule.AmountMinor(), wantPositive)
	m, err := money.NewMoney(amountMinor, account.Currency())
	if err != nil {
		return ledger.Account{}, ledger.Category{}, money.Money{}, errs.New(errs.Internal).Wrap(err)
	}
	return account, category, m, nil
}

// MaterialiseOccurrence implements issue #279's materialisation use case.
func (s *Service) MaterialiseOccurrence(ctx context.Context, cmd MaterialiseOccurrenceCommand) (MaterialiseOccurrenceResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return MaterialiseOccurrenceResult{}, err
	}

	occurrence, err := s.ScheduledOccurrences.Get(ctx, cmd.ActorID, cmd.OccurrenceID)
	if err != nil {
		return MaterialiseOccurrenceResult{}, attachField(err, "occurrence_id")
	}

	rule, err := s.RecurringRules.Get(ctx, cmd.ActorID, occurrence.RuleID())
	if err != nil {
		return MaterialiseOccurrenceResult{}, err
	}

	account, category, m, err := s.resolveOccurrenceMoney(ctx, cmd.ActorID, rule)
	if err != nil {
		return MaterialiseOccurrenceResult{}, err
	}

	// rule.AmountMinor() is always strictly positive (ADR-0014: the
	// direction comes from the category's kind, not the sign), but
	// ledger.NewOutflow/NewInflow each require every posting's sign to
	// already match the kind of transaction being built — the same
	// wantPositive dispatch buildOutflowOrInflowPosting uses for
	// user-entered amounts.
	wantPositive := category.Kind() == ledger.CategoryKindIncome
	categoryID := category.ID()
	posting, err := ledger.NewPosting(s.IDs.NewID(), account.ID(), m, &categoryID, 0)
	if err != nil {
		return MaterialiseOccurrenceResult{}, errs.New(errs.Internal).Wrap(err)
	}

	txnID := s.IDs.NewID()
	var txn ledger.Transaction
	if wantPositive {
		txn, err = ledger.NewInflow(txnID, cmd.ActorID, occurrence.OccurrenceDate(), rule.Description(), []ledger.Posting{posting})
	} else {
		txn, err = ledger.NewOutflow(txnID, cmd.ActorID, occurrence.OccurrenceDate(), rule.Description(), []ledger.Posting{posting})
	}
	if err != nil {
		return MaterialiseOccurrenceResult{}, wrapLedgerError(err)
	}

	materialised, err := occurrence.MarkMaterialised(txnID)
	if err != nil {
		return MaterialiseOccurrenceResult{}, wrapRecurringError(err)
	}

	if err := s.RecurringMaterializations.Materialize(ctx, cmd.ActorID, materialised, txn); err != nil {
		return MaterialiseOccurrenceResult{}, err
	}
	return MaterialiseOccurrenceResult{Transaction: txn, Occurrence: materialised}, nil
}

// SkipOccurrenceCommand records that a pending occurrence's firing was
// deliberately not recorded — data-model.md §11's "skipped: money never
// moved and never will for this date." No transaction is created.
type SkipOccurrenceCommand struct {
	ActorID      string
	OccurrenceID string
}

// SkipOccurrenceResult wraps the occurrence a skip use case affected.
type SkipOccurrenceResult struct {
	Occurrence recurring.ScheduledOccurrence
}

// SkipOccurrence implements issue #279's skip use case.
func (s *Service) SkipOccurrence(ctx context.Context, cmd SkipOccurrenceCommand) (SkipOccurrenceResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return SkipOccurrenceResult{}, err
	}

	occurrence, err := s.ScheduledOccurrences.Get(ctx, cmd.ActorID, cmd.OccurrenceID)
	if err != nil {
		return SkipOccurrenceResult{}, attachField(err, "occurrence_id")
	}

	skipped, err := occurrence.MarkSkipped()
	if err != nil {
		return SkipOccurrenceResult{}, wrapRecurringError(err)
	}

	if err := s.ScheduledOccurrences.Update(ctx, cmd.ActorID, skipped); err != nil {
		return SkipOccurrenceResult{}, err
	}
	return SkipOccurrenceResult{Occurrence: skipped}, nil
}
