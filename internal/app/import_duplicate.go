package app

import (
	"context"
	"strings"
	"unicode"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/importing"
	"github.com/anirudhgray/bodger/internal/domain/money"
	"github.com/anirudhgray/bodger/internal/ports"
)

// duplicateDateWindowDays is ADR-0008's tier-2 heuristic window and cross-
// account transfer window alike: "booked_date within +/-3 days."
const duplicateDateWindowDays = 3

// descriptionSimilarityThreshold is how much of two descriptions' token
// sets must overlap (a Jaccard index) for tier-2 to call them "similar" —
// ADR-0008's own qualitative wording, made concrete and testable here. 0.5
// catches an obvious rewording of the same merchant ("STARBUCKS #4521" vs
// "Starbucks Coffee") while still requiring genuine overlap rather than a
// single shared common word.
const descriptionSimilarityThreshold = 0.5

// findExactDuplicate implements ADR-0008's tier-1 check: "(account_id,
// external_id) is unique." A match here is a definite duplicate and gets
// auto-excluded by the caller (buildImportRecord) — never surfaced for
// review, per ADR-0008.
//
// This looks only at already-committed transactions on accountID —
// nothing in the current batch (or any other still-staged batch) has
// reached the ledger yet (ADR-0008: "nothing before commit affects... the
// ledger"), so a same-file repeat of one external_id cannot be caught this
// way. That's a real, documented gap for this issue rather than an
// oversight: DuplicateMatch's matched side is a transaction ID, not
// another ImportRecord's, so a record-to-record collision has no existing
// shape to fit into and isn't attempted here — see this issue's final
// report.
func (s *Service) findExactDuplicate(ctx context.Context, actorID, accountID, externalID string) (string, bool, error) {
	if externalID == "" {
		return "", false, nil
	}
	matches, err := s.Transactions.List(ctx, actorID, ports.TransactionFilter{AccountID: accountID, ExternalID: externalID})
	if err != nil {
		return "", false, err
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	return matches[0].ID(), true, nil
}

// findSuspectedDuplicate implements ADR-0008's tier-2 heuristic: same
// account, exact amount and currency, booked_date within
// duplicateDateWindowDays days, and a similar description. The caller
// (buildImportRecord) only ever attaches the result as a DuplicateMatch
// for the user to resolve — this function itself makes no exclusion
// decision, matching ADR-0008's "the system never silently merges a
// tier-2 match."
//
// When more than one existing transaction matches — ADR-0008's own
// false-positive example, "two genuine identical coffees on the same
// day" — the first one TransactionRepository.List returns becomes the
// cited candidate. Which one is cited doesn't change the outcome: every
// match is surfaced identically for review, never auto-excluded, so an
// arbitrary but deterministic choice is fine here, and both of the new
// import's coffee rows still end up as two separate, independently
// reviewable ImportRecords rather than one silently dropped.
func (s *Service) findSuspectedDuplicate(ctx context.Context, actorID, accountID string, amount money.Money, bookedDate domain.Date, description string) (string, bool, error) {
	from := addDays(bookedDate, -duplicateDateWindowDays)
	to := addDays(bookedDate, duplicateDateWindowDays)
	abs := amount.Abs().AmountString()

	candidates, err := s.Transactions.List(ctx, actorID, ports.TransactionFilter{
		AccountID:  accountID,
		FromDate:   &from,
		ToDate:     &to,
		Currencies: []string{amount.Currency()},
		AmountMin:  abs,
		AmountMax:  abs,
	})
	if err != nil {
		return "", false, err
	}

	for _, txn := range candidates {
		for _, p := range txn.Postings() {
			if p.AccountID() != accountID {
				continue
			}
			if !p.Amount().Equal(amount) {
				continue
			}
			if !descriptionsSimilar(description, txn.Description()) {
				continue
			}
			return txn.ID(), true, nil
		}
	}
	return "", false, nil
}

// findTransferCandidateRecord implements ADR-0008's cross-account transfer
// heuristic: "an outflow on one account and an inflow on another are the
// same movement... uses the same heuristic across accounts" — opposite
// signs, same magnitude and currency, booked_date within
// duplicateDateWindowDays days. Like tier-2, this is "proposed, never
// applied automatically": the caller only ever records the match as a
// TransferCandidateRecordID hint, and nothing about commit behaviour
// changes because of it (that decision belongs to issue #211).
//
// This looks at other ImportRecords the same actor has already staged, in
// a different batch targeting a different account, and still short of
// commit (staged or reviewed) — the shape a fresh, same-sitting import of
// both sides of a transfer actually takes: neither leg is a committed
// transaction yet for a duplicate-style lookup to find. Matching against
// already-committed transactions on other accounts (the other leg from an
// earlier session) is a natural extension this issue doesn't attempt,
// for the same matched-side-is-a-record-not-a-transaction reason
// findExactDuplicate's doc comment gives — see this issue's final report.
func (s *Service) findTransferCandidateRecord(ctx context.Context, actorID, excludeBatchID, accountID string, amount money.Money, bookedDate domain.Date) (string, bool, error) {
	batches, err := s.ImportBatches.List(ctx, actorID)
	if err != nil {
		return "", false, err
	}

	from := addDays(bookedDate, -duplicateDateWindowDays)
	to := addDays(bookedDate, duplicateDateWindowDays)
	opposite := amount.Negate()

	for _, batch := range batches {
		if batch.ID() == excludeBatchID || batch.TargetAccountID() == accountID {
			continue
		}
		if batch.Status() != importing.ImportBatchStatusStaged && batch.Status() != importing.ImportBatchStatusReviewed {
			continue
		}

		records, err := s.ImportRecords.ListByImportBatch(ctx, actorID, batch.ID())
		if err != nil {
			return "", false, err
		}
		for _, rec := range records {
			if rec.Status() == importing.ImportRecordStatusExcluded || rec.Status() == importing.ImportRecordStatusCommitted {
				continue
			}
			if !rec.Amount().Equal(opposite) {
				continue
			}
			if rec.BookedDate().Before(from) || rec.BookedDate().After(to) {
				continue
			}
			return rec.ID(), true, nil
		}
	}
	return "", false, nil
}

// descriptionsSimilar reports whether a and b share enough tokens to count
// as ADR-0008's "similar description" — a Jaccard index (intersection over
// union of each description's lowercased word/number tokens) at or above
// descriptionSimilarityThreshold. Two blank or entirely-punctuation
// descriptions never count as similar to anything, including each other.
func descriptionsSimilar(a, b string) bool {
	ta := descriptionTokens(a)
	tb := descriptionTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return false
	}

	intersection := 0
	for tok := range ta {
		if tb[tok] {
			intersection++
		}
	}
	union := len(ta) + len(tb) - intersection
	if union == 0 {
		return false
	}
	return float64(intersection)/float64(union) >= descriptionSimilarityThreshold
}

// descriptionTokens splits s into lowercased runs of letters/digits,
// dropping punctuation and whitespace as separators — "STARBUCKS #4521"
// tokenises to {"starbucks", "4521"}, matching "Starbucks Coffee 4521"'s
// shared "starbucks" and "4521" tokens despite the different punctuation
// and casing.
func descriptionTokens(s string) map[string]bool {
	tokens := map[string]bool{}
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			tokens[strings.ToLower(b.String())] = true
			b.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}
