package app

import (
	"context"
	"strings"
	"time"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/recurring"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// generationHorizonMonths is ADR-0014's fixed generation window: occurrences
// are projected out to today + this many months, clipped by a rule's own
// ends_on (RecurringRule.OccurrencesBetween does that clipping). Twelve
// months is chosen so a yearly rule produces at least one row and a
// forecast has a full cycle to draw.
const generationHorizonMonths = 12

// GenerateOccurrencesCommand runs ADR-0014's explicit, idempotent occurrence
// generation — never a background job, always an action a surface took
// (issue #278). RuleID is optional: set it to (re)generate just one rule's
// occurrences (e.g. right after creating or editing it), or leave it empty
// to refresh every active rule the actor owns in one call, which is what a
// future "bodger recurring refresh" (surface wiring, issue #281) will call.
type GenerateOccurrencesCommand struct {
	ActorID string
	RuleID  string
}

// GenerateOccurrencesResult reports the occurrences a generation call
// actually created — never ones that already existed, since generation is
// idempotent (ADR-0014): re-running it over an overlapping window adds
// nothing new.
type GenerateOccurrencesResult struct {
	Created []recurring.ScheduledOccurrence
}

// GenerateOccurrences implements issue #278's projection use case: for each
// active rule in scope, it ensures pending occurrences exist out to the
// generation horizon, without duplicating a date that already has an
// occurrence of any status.
func (s *Service) GenerateOccurrences(ctx context.Context, cmd GenerateOccurrencesCommand) (GenerateOccurrencesResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return GenerateOccurrencesResult{}, err
	}

	var rules []recurring.RecurringRule
	if cmd.RuleID != "" {
		rule, err := s.RecurringRules.Get(ctx, cmd.ActorID, cmd.RuleID)
		if err != nil {
			return GenerateOccurrencesResult{}, attachField(err, "rule_id")
		}
		rules = []recurring.RecurringRule{rule}
	} else {
		all, err := s.RecurringRules.List(ctx, cmd.ActorID)
		if err != nil {
			return GenerateOccurrencesResult{}, err
		}
		rules = all
	}

	today, err := normalize.DateOf("", s.Clock, s.Config.UserTimezone)
	if err != nil {
		return GenerateOccurrencesResult{}, err
	}
	horizon := addMonthsToDate(today, generationHorizonMonths)

	var created []recurring.ScheduledOccurrence
	for _, rule := range rules {
		if rule.Archived() {
			continue
		}
		newOnes, err := s.generateForRule(ctx, cmd.ActorID, rule, rule.StartsOn(), horizon)
		if err != nil {
			return GenerateOccurrencesResult{}, err
		}
		created = append(created, newOnes...)
	}
	return GenerateOccurrencesResult{Created: created}, nil
}

// generateForRule projects rule's firings within [from, horizon]
// (OccurrencesBetween clips from up to the rule's own StartsOn if it's
// earlier, and to/EndsOn if that's sooner), skips any date that already has
// an occurrence in any status, and persists the missing ones in one
// CreateBatch call.
//
// from is StartsOn for GenerateOccurrences' general "ensure occurrences
// exist" path (ADR-0014: "generation starts at the rule's own starts_on
// rather than today ... so a rule backdated to an existing standing order
// surfaces its past firings for review"), and today for
// regenerateAfterScheduleChange's narrower "future occurrences" scope —
// passing the wrong one for the latter would insert brand-new *past*
// occurrences under a schedule the rule never actually ran under before
// today.
//
// The dedup check considers every status, not just pending: occurrence_date
// is unique per rule regardless of status, so a date that's already
// materialised or skipped must never be regenerated as a fresh pending row.
// The schema's own (rule_id, occurrence_date) uniqueness constraint is
// ADR-0014's backstop for this, not the primary mechanism — this scan is.
func (s *Service) generateForRule(ctx context.Context, actorID string, rule recurring.RecurringRule, from, horizon domain.Date) ([]recurring.ScheduledOccurrence, error) {
	dates := rule.OccurrencesBetween(from, horizon)
	if len(dates) == 0 {
		return nil, nil
	}

	existing, err := s.ScheduledOccurrences.List(ctx, actorID, ports.ScheduledOccurrenceFilter{RuleID: rule.ID()})
	if err != nil {
		return nil, err
	}
	have := make(map[domain.Date]bool, len(existing))
	for _, o := range existing {
		have[o.OccurrenceDate()] = true
	}

	var toCreate []recurring.ScheduledOccurrence
	for _, d := range dates {
		if have[d] {
			continue
		}
		occurrence, err := recurring.NewScheduledOccurrence(s.IDs.NewID(), rule.ID(), d)
		if err != nil {
			// Unreachable: s.IDs.NewID() and rule.ID() are always non-empty
			// for a rule already fetched from the repository.
			return nil, errs.New(errs.Internal).Explain("Could not build a scheduled occurrence.").Wrap(err)
		}
		toCreate = append(toCreate, occurrence)
	}
	if len(toCreate) == 0 {
		return nil, nil
	}
	if err := s.ScheduledOccurrences.CreateBatch(ctx, actorID, toCreate); err != nil {
		return nil, err
	}
	return toCreate, nil
}

// regenerateAfterScheduleChange implements ADR-0014's "when a rule's
// schedule changes, its pending future occurrences are discarded and
// regenerated": called from UpdateRecurringRule once it has confirmed the
// schedule actually changed. Only occurrences on or after today are
// touched — a materialised or skipped occurrence is untouched by
// DeletePending's own contract, and a past-dated pending occurrence (an
// overdue one nobody has resolved yet) is left as-is rather than
// retroactively recomputed, per the ADR's own wording ("future").
func (s *Service) regenerateAfterScheduleChange(ctx context.Context, actorID string, rule recurring.RecurringRule) error {
	today, err := normalize.DateOf("", s.Clock, s.Config.UserTimezone)
	if err != nil {
		return err
	}
	if err := s.ScheduledOccurrences.DeletePending(ctx, actorID, rule.ID(), today); err != nil {
		return err
	}
	horizon := addMonthsToDate(today, generationHorizonMonths)
	_, err = s.generateForRule(ctx, actorID, rule, today, horizon)
	return err
}

// scheduleEqual reports whether a and b describe the same firing pattern —
// same frequency, interval, and whichever positional field(s) that
// frequency uses. recurring.Schedule has no Equal method of its own since
// nothing in internal/domain/recurring needs one; UpdateRecurringRule is
// the one caller that needs to detect "did the schedule actually change"
// to decide whether occurrence regeneration is needed at all.
func scheduleEqual(a, b recurring.Schedule) bool {
	if a.Frequency() != b.Frequency() || a.Interval() != b.Interval() {
		return false
	}
	switch a.Frequency() {
	case recurring.FrequencyWeekly:
		aw, _ := a.Weekday()
		bw, _ := b.Weekday()
		return aw == bw
	case recurring.FrequencyMonthly:
		ad, _ := a.DayOfMonth()
		bd, _ := b.DayOfMonth()
		return ad == bd
	case recurring.FrequencyYearly:
		am, _ := a.Month()
		bm, _ := b.Month()
		ad, _ := a.DayOfMonth()
		bd, _ := b.DayOfMonth()
		return am == bm && ad == bd
	default:
		return false
	}
}

// ListScheduledOccurrencesQuery lists ScheduledOccurrence rows actorID
// owns, optionally narrowed to one rule, one status, and/or an inclusive
// occurrence-date range — a thin app-layer wrapper over
// ports.ScheduledOccurrenceRepository.List/ScheduledOccurrenceFilter.
//
// This is issue #281's own gap fill: the repository-level filter already
// existed (internal/ports/scheduled_occurrence.go), but no app-layer
// query exposed it to a surface, which would have meant REST, CLI, and
// MCP each reimplementing the same status/date filtering independently —
// exactly what CLAUDE.md's normalize-once rule and ADR-0005 exist to
// prevent. RuleID, when set, is resolved the same actor-scoped way every
// other recurring-rule use case resolves one (RecurringRules.Get), which
// doubles as this query's authorisation check for that field the same
// way resolveOwnedAccount/resolveOwnedCategory does elsewhere — the
// underlying ScheduledOccurrenceRepository.List is already actor-scoped
// on its own, but resolving the rule first turns "that rule isn't yours"
// into a clear NotFound instead of a silently empty result.
type ListScheduledOccurrencesQuery struct {
	ActorID  string
	RuleID   string
	Status   string
	FromDate string
	ToDate   string
}

// ListScheduledOccurrencesResult is ListScheduledOccurrences' result, in
// the repository's ascending occurrence_date order.
type ListScheduledOccurrencesResult struct {
	Occurrences []recurring.ScheduledOccurrence
}

// ListScheduledOccurrences implements the "list occurrences, with a
// status filter" read issue #281's REST scope calls for.
func (s *Service) ListScheduledOccurrences(ctx context.Context, q ListScheduledOccurrencesQuery) (ListScheduledOccurrencesResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ListScheduledOccurrencesResult{}, err
	}

	filter := ports.ScheduledOccurrenceFilter{}

	if strings.TrimSpace(q.RuleID) != "" {
		rule, err := s.RecurringRules.Get(ctx, q.ActorID, q.RuleID)
		if err != nil {
			return ListScheduledOccurrencesResult{}, attachField(err, "rule_id")
		}
		filter.RuleID = rule.ID()
	}

	status, err := parseOccurrenceStatus(q.Status)
	if err != nil {
		return ListScheduledOccurrencesResult{}, err
	}
	filter.Status = status

	from, err := optionalDate(q.FromDate, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return ListScheduledOccurrencesResult{}, attachField(err, "from_date")
	}
	filter.FromDate = from

	to, err := optionalDate(q.ToDate, s.Clock, s.Config.UserTimezone)
	if err != nil {
		return ListScheduledOccurrencesResult{}, attachField(err, "to_date")
	}
	filter.ToDate = to

	occurrences, err := s.ScheduledOccurrences.List(ctx, q.ActorID, filter)
	if err != nil {
		return ListScheduledOccurrencesResult{}, err
	}
	return ListScheduledOccurrencesResult{Occurrences: occurrences}, nil
}

// parseOccurrenceStatus validates raw against recurring's closed set of
// occurrence statuses (recurring.OccurrenceStatus), the same
// parseAccountKind/parseCategoryKind/parseTransactionKind shape
// helpers.go already uses for every other closed-enum filter field. ""
// means "no status filter" — every status — matching
// ports.ScheduledOccurrenceFilter's own zero-value convention.
func parseOccurrenceStatus(raw string) (recurring.OccurrenceStatus, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	status := recurring.OccurrenceStatus(strings.ToLower(strings.TrimSpace(raw)))
	switch status {
	case recurring.OccurrenceStatusPending, recurring.OccurrenceStatusMaterialised, recurring.OccurrenceStatusSkipped:
		return status, nil
	default:
		return "", errs.New(errs.InvalidInput).
			Explain("%q isn't a valid occurrence status.", raw).
			Field("status").
			With("valid_statuses", []string{"pending", "materialised", "skipped"})
	}
}

// addMonthsToDate returns the calendar date `months` months after d, via
// Go's own time.Date/AddDate month-rollover normalisation (e.g. 31 January
// + 1 month lands on 2 or 3 March). This is the generation horizon's own
// edge, not a rule's firing date, so it has no reason to reproduce
// recurring.Schedule's ADR-0014 clamp-to-last-day rule — a horizon landing
// a few days into the "wrong" month costs nothing, since OccurrencesBetween
// only uses it as an inclusive upper bound.
func addMonthsToDate(d domain.Date, months int) domain.Date {
	t := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, months, 0)
	out, err := domain.NewDate(t.Year(), t.Month(), t.Day())
	if err != nil {
		// Unreachable: t came from time.Date/AddDate, which only ever
		// produces real calendar dates.
		return d
	}
	return out
}
