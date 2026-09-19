// This file implements issue #281's `bodger recurring` surface, wired onto
// internal/app/recurring_rules.go's CRUD use cases (issue #277),
// internal/app/recurring_occurrences.go's ListScheduledOccurrences query
// (issue #278, extended by #281 itself), internal/app/recurring_occurrences_actions.go's
// materialise/skip use cases (issue #279), and internal/app/forecast.go's
// projected-activity query (issue #280) — the same one-command,
// one-application-call way as every other command in this package, following
// budgets.go's conventions.
//
// Two deliberate extensions past issue #281's own literal command list:
//
//   - `bodger recurring forecast` isn't named in the issue's CLI bullet
//     (only its REST and MCP bullets name a forecast operation), but
//     leaving it out would mean the CLI is the one surface that can't
//     answer "what's coming up" — exactly the kind of one-surface-behind
//     gap ADR-0005's "one coherent set of capabilities, several thin
//     surfaces" exists to prevent. Added for parity with GET
//     /api/v1/forecast and the MCP get_forecast tool.
//   - There is no `bodger recurring show <rule-id>` (a single-rule get).
//     This one is deliberately *not* added, unlike forecast: the issue's
//     CLI and MCP bullets both consistently omit a single-rule get
//     (leaving it to REST's GET /api/v1/recurring-rules/{id} alone), so
//     omitting it here follows the issue's own scope rather than
//     guessing past it. `bodger recurring list` already shows every
//     field a single rule's own view would.
package cli

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// ---- shared views ----

// scheduleView is a RecurringRule's firing pattern, mirroring
// internal/surface/http/recurring.go's own scheduleView field for field —
// only the fields the declared frequency actually uses are populated.
type scheduleView struct {
	Frequency  string `json:"frequency"`
	Interval   int    `json:"interval"`
	Weekday    *int   `json:"weekday,omitempty"`
	DayOfMonth *int   `json:"day_of_month,omitempty"`
	Month      *int   `json:"month,omitempty"`
}

func (s scheduleView) String() string {
	switch s.Frequency {
	case "weekly":
		wd := "?"
		if s.Weekday != nil {
			wd = time.Weekday(*s.Weekday).String()
		}
		return fmt.Sprintf("every %d week(s) on %s", s.Interval, wd)
	case "monthly":
		dom := 0
		if s.DayOfMonth != nil {
			dom = *s.DayOfMonth
		}
		return fmt.Sprintf("every %d month(s) on day %d", s.Interval, dom)
	case "yearly":
		month, dom := "?", 0
		if s.Month != nil {
			month = time.Month(*s.Month).String()
		}
		if s.DayOfMonth != nil {
			dom = *s.DayOfMonth
		}
		return fmt.Sprintf("every %d year(s) on %s %d", s.Interval, month, dom)
	default:
		return s.Frequency
	}
}

// recurringRuleView is `bodger recurring`'s rendered shape for a
// RecurringRule, mirroring internal/surface/http/recurring.go's own
// recurringRuleView field for field. Amount is rendered via
// app.RecurringRuleAmount, since a RecurringRule carries no currency of
// its own and this package may not import internal/domain/money to build
// one itself.
type recurringRuleView struct {
	ID          string       `json:"id"`
	AccountID   string       `json:"account_id"`
	CategoryID  string       `json:"category_id"`
	Amount      string       `json:"amount"`
	Currency    string       `json:"currency"`
	Description string       `json:"description"`
	Schedule    scheduleView `json:"schedule"`
	StartsOn    string       `json:"starts_on"`
	EndsOn      string       `json:"ends_on,omitempty"`
	Archived    bool         `json:"archived"`
	ArchivedAt  string       `json:"archived_at,omitempty"`
}

func recurringRuleViewFrom(r app.RecurringRuleResult) (recurringRuleView, error) {
	rule := r.Rule
	schedule := rule.Schedule()

	v := recurringRuleView{
		ID:          rule.ID(),
		AccountID:   rule.AccountID(),
		CategoryID:  rule.CategoryID(),
		Currency:    r.Currency,
		Description: rule.Description(),
		Schedule: scheduleView{
			Frequency: string(schedule.Frequency()),
			Interval:  schedule.Interval(),
		},
		StartsOn: rule.StartsOn().String(),
		Archived: rule.Archived(),
	}
	if wd, ok := schedule.Weekday(); ok {
		w := int(wd)
		v.Schedule.Weekday = &w
	}
	if dom, ok := schedule.DayOfMonth(); ok {
		v.Schedule.DayOfMonth = &dom
	}
	if m, ok := schedule.Month(); ok {
		mm := int(m)
		v.Schedule.Month = &mm
	}
	if d, ok := rule.EndsOn(); ok {
		v.EndsOn = d.String()
	}
	if d, ok := rule.ArchivedAt(); ok {
		v.ArchivedAt = d.String()
	}

	amount, err := app.RecurringRuleAmount(r.Currency, rule)
	if err != nil {
		return recurringRuleView{}, err
	}
	v.Amount = amount.AmountString()
	return v, nil
}

func printRecurringRule(w io.Writer, v recurringRuleView) {
	archived := ""
	if v.Archived {
		archived = " — archived"
	}
	_, _ = fmt.Fprintf(w, "%s: %s %s, %s, starts %s%s\n", v.Description, v.Amount, v.Currency, v.Schedule.String(), v.StartsOn, archived)
	if v.EndsOn != "" {
		_, _ = fmt.Fprintf(w, "ends %s\n", v.EndsOn)
	}
	_, _ = fmt.Fprintf(w, "id: %s  account: %s  category: %s\n", v.ID, v.AccountID, v.CategoryID)
}

func printRecurringRuleTable(w io.Writer, views []recurringRuleView) {
	if len(views) == 0 {
		_, _ = fmt.Fprintln(w, "No recurring rules yet. Add one with `bodger recurring create`.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tDESCRIPTION\tAMOUNT\tSCHEDULE\tSTARTS ON\tARCHIVED")
	for _, v := range views {
		archived := "no"
		if v.Archived {
			archived = "yes"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s %s\t%s\t%s\t%s\n", v.ID, v.Description, v.Amount, v.Currency, v.Schedule.String(), v.StartsOn, archived)
	}
	_ = tw.Flush()
}

// scheduleFlags holds the flag variables `recurring create` and
// `recurring update` bind identically for a rule's schedule.
type scheduleFlags struct {
	frequency  string
	interval   int
	weekday    int
	dayOfMonth int
	month      int
}

func (f *scheduleFlags) input() app.RecurringScheduleInput {
	return app.RecurringScheduleInput{
		Frequency:  f.frequency,
		Interval:   f.interval,
		Weekday:    time.Weekday(f.weekday),
		DayOfMonth: f.dayOfMonth,
		Month:      time.Month(f.month),
	}
}

func bindScheduleFlags(cmd *cobra.Command) *scheduleFlags {
	f := &scheduleFlags{}
	cmd.Flags().StringVar(&f.frequency, "frequency", "", `the recurrence frequency: "weekly", "monthly", or "yearly" (required)`)
	cmd.Flags().IntVar(&f.interval, "interval", 1, "fire every this many weeks/months/years")
	cmd.Flags().IntVar(&f.weekday, "weekday", 0, "0 (Sunday) - 6 (Saturday); required for a weekly schedule")
	cmd.Flags().IntVar(&f.dayOfMonth, "day-of-month", 0, "1-31; required for a monthly or yearly schedule")
	cmd.Flags().IntVar(&f.month, "month", 0, "1 (January) - 12 (December); required for a yearly schedule")
	return f
}

// ---- recurring create/update/archive/list ----

func newRecurringCreateCmd(factory ServiceFactory) *cobra.Command {
	var startsOn, endsOn string
	var schedule *scheduleFlags
	cmd := &cobra.Command{
		Use:   "create <account> <category> <amount> <description>",
		Short: "Create a recurring rule",
		Long: "Create a recurring rule's template only - no occurrence is generated by this call.\n\n" +
			"The direction of the money (an outflow or an inflow) follows from the category's own kind.",
		Args: cobra.ExactArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.CreateRecurringRule(ctx, app.CreateRecurringRuleCommand{
				ActorID:     ports.SeededUserID,
				AccountRef:  args[0],
				CategoryRef: args[1],
				Amount:      args[2],
				Description: args[3],
				Schedule:    schedule.input(),
				StartsOn:    startsOn,
				EndsOn:      endsOn,
			})
			if err != nil {
				return err
			}
			view, err := recurringRuleViewFrom(result)
			if err != nil {
				return err
			}
			return render(cmd, view, func(w io.Writer) { printRecurringRule(w, view) })
		},
	}
	schedule = bindScheduleFlags(cmd)
	cmd.Flags().StringVar(&startsOn, "starts-on", "", "the first date the rule may fire on (defaults to today)")
	cmd.Flags().StringVar(&endsOn, "ends-on", "", "the last date the rule may fire on (omit for a rule that runs indefinitely)")
	return cmd
}

// newRecurringUpdateCmd builds "recurring update". Every editable field is
// always set together, mirroring `budgets update`'s full-replacement
// contract: internal/app.UpdateRecurringRuleCommand has no "leave this
// field alone" option, so omitting --ends-on clears it to "runs
// indefinitely", not to the rule's existing value. The rule's account,
// category, and starts-on date can't change — archive it and create a new
// one instead.
func newRecurringUpdateCmd(factory ServiceFactory) *cobra.Command {
	var endsOn string
	var schedule *scheduleFlags
	cmd := &cobra.Command{
		Use:   "update <rule-id> <amount> <description>",
		Short: "Update a recurring rule's amount, description, schedule, and end date",
		Long: "Update a recurring rule's amount, description, schedule, and end date.\n\n" +
			"Every field is always set together: if you omit --ends-on, it clears to \"runs " +
			"indefinitely\", not to the rule's existing end date. Pass the rule's current " +
			"--ends-on back explicitly to leave it unchanged. If the schedule's own firing " +
			"pattern actually changes, the rule's pending future occurrences are discarded and " +
			"regenerated.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.UpdateRecurringRule(ctx, app.UpdateRecurringRuleCommand{
				ActorID:     ports.SeededUserID,
				RuleID:      args[0],
				Amount:      args[1],
				Description: args[2],
				Schedule:    schedule.input(),
				EndsOn:      endsOn,
			})
			if err != nil {
				return err
			}
			view, err := recurringRuleViewFrom(result)
			if err != nil {
				return err
			}
			return render(cmd, view, func(w io.Writer) { printRecurringRule(w, view) })
		},
	}
	schedule = bindScheduleFlags(cmd)
	cmd.Flags().StringVar(&endsOn, "ends-on", "", "the last date the rule may fire on (omit to clear to \"runs indefinitely\")")
	return cmd
}

func newRecurringArchiveCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <rule-id>",
		Short: "Archive a recurring rule, cancelling its still-pending occurrences",
		Long: "Archive a recurring rule: it stops generating any further occurrences, and any " +
			"still-pending occurrence is cancelled (removed) rather than left dangling. Its " +
			"history - including already materialised or skipped occurrences - remains fully " +
			"queryable.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ArchiveRecurringRule(ctx, app.ArchiveRecurringRuleCommand{ActorID: ports.SeededUserID, RuleID: args[0]})
			if err != nil {
				return err
			}
			view, err := recurringRuleViewFrom(result)
			if err != nil {
				return err
			}
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Archived recurring rule %q.\n", view.Description)
			})
		},
	}
}

func newRecurringListCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your recurring rules",
		Long:  "List your recurring rules. Includes archived rules - filtering to \"current\" is left to you.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ListRecurringRules(ctx, app.ListRecurringRulesQuery{ActorID: ports.SeededUserID})
			if err != nil {
				return err
			}
			views := make([]recurringRuleView, len(result.Rules))
			for i, rule := range result.Rules {
				view, err := recurringRuleViewFrom(app.RecurringRuleResult{Rule: rule, Currency: result.Currencies[rule.ID()]})
				if err != nil {
					return err
				}
				views[i] = view
			}
			return render(cmd, views, func(w io.Writer) { printRecurringRuleTable(w, views) })
		},
	}
}

// ---- recurring occurrences list ----

// scheduledOccurrenceView is one ScheduledOccurrence's rendered shape,
// mirroring internal/surface/http/recurring.go's own scheduledOccurrenceView
// field for field — no account, no currency, no amount, since an
// occurrence is a projection, never money that moved (data-model.md §11).
type scheduledOccurrenceView struct {
	ID             string `json:"id"`
	RuleID         string `json:"rule_id"`
	OccurrenceDate string `json:"occurrence_date"`
	Status         string `json:"status"`
	TransactionID  string `json:"transaction_id,omitempty"`
}

func printScheduledOccurrenceTable(w io.Writer, views []scheduledOccurrenceView) {
	if len(views) == 0 {
		_, _ = fmt.Fprintln(w, "No matching occurrences.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tRULE ID\tDATE\tSTATUS\tTRANSACTION ID")
	for _, v := range views {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", v.ID, v.RuleID, v.OccurrenceDate, v.Status, v.TransactionID)
	}
	_ = tw.Flush()
}

func printScheduledOccurrence(w io.Writer, v scheduledOccurrenceView) {
	_, _ = fmt.Fprintf(w, "occurrence %s: rule %s, %s, %s\n", v.ID, v.RuleID, v.OccurrenceDate, v.Status)
	if v.TransactionID != "" {
		_, _ = fmt.Fprintf(w, "transaction: %s\n", v.TransactionID)
	}
}

func newRecurringOccurrencesCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "occurrences",
		Short: "List scheduled occurrences",
	}
	cmd.AddCommand(newRecurringOccurrencesListCmd(factory))
	return cmd
}

func newRecurringOccurrencesListCmd(factory ServiceFactory) *cobra.Command {
	var ruleID, status, from, to string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List scheduled occurrences, optionally by rule, status, and date range",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ListScheduledOccurrences(ctx, app.ListScheduledOccurrencesQuery{
				ActorID: ports.SeededUserID, RuleID: ruleID, Status: status, FromDate: from, ToDate: to,
			})
			if err != nil {
				return err
			}
			views := make([]scheduledOccurrenceView, len(result.Occurrences))
			for i, occ := range result.Occurrences {
				txID, _ := occ.TransactionID()
				views[i] = scheduledOccurrenceView{
					ID: occ.ID(), RuleID: occ.RuleID(), OccurrenceDate: occ.OccurrenceDate().String(),
					Status: string(occ.Status()), TransactionID: txID,
				}
			}
			return render(cmd, views, func(w io.Writer) { printScheduledOccurrenceTable(w, views) })
		},
	}
	cmd.Flags().StringVar(&ruleID, "rule", "", "only include occurrences of this rule")
	cmd.Flags().StringVar(&status, "status", "", `only include occurrences in this status: "pending", "materialised", or "skipped"`)
	cmd.Flags().StringVar(&from, "from", "", "only include occurrences on or after this date")
	cmd.Flags().StringVar(&to, "to", "", "only include occurrences on or before this date")
	return cmd
}

// ---- recurring materialise/skip ----

// materialiseOccurrenceView is `bodger recurring materialise`'s rendered
// shape, mirroring internal/surface/http/recurring.go's own
// materialiseOccurrenceView field for field: the transaction it produced,
// and the occurrence's own updated state, both under the same "occurrence"/
// "transaction" keys the REST and MCP surfaces use — the same
// cross-surface JSON-shape agreement every other command in this package
// keeps, proven by internal/surface/conformance's own MCP/HTTP legs.
type materialiseOccurrenceView struct {
	Occurrence  scheduledOccurrenceView `json:"occurrence"`
	Transaction transactionView         `json:"transaction"`
}

func newRecurringMaterialiseCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "materialise <occurrence-id>",
		Short: "Turn a pending occurrence into a real transaction",
		Long: "Turn a pending occurrence into a real transaction, always using the occurrence's " +
			"owning rule's current amount, account, category, and description, and the " +
			"occurrence's own projected date - there is no way to override any of these for this " +
			"one materialisation. Materialise with the projected fields, then `bodger transactions " +
			"edit` the result, to record it with a different amount or date.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.MaterialiseOccurrence(ctx, app.MaterialiseOccurrenceCommand{
				ActorID: ports.SeededUserID, OccurrenceID: args[0],
			})
			if err != nil {
				return err
			}
			txID, _ := result.Occurrence.TransactionID()
			view := materialiseOccurrenceView{
				Occurrence: scheduledOccurrenceView{
					ID: result.Occurrence.ID(), RuleID: result.Occurrence.RuleID(),
					OccurrenceDate: result.Occurrence.OccurrenceDate().String(),
					Status:         string(result.Occurrence.Status()), TransactionID: txID,
				},
				Transaction: transactionViewFrom(app.TransactionResult{Transaction: result.Transaction}),
			}
			return render(cmd, view, func(w io.Writer) {
				printScheduledOccurrence(w, view.Occurrence)
				_, _ = fmt.Fprintf(w, "Recorded %q for %s.\n", result.Transaction.Description(), result.Transaction.BookedDate().String())
			})
		},
	}
}

// newRecurringSkipCmd builds "recurring skip". Aliased as "skip" rather
// than nested under "occurrences" alongside "occurrences list", matching
// issue #281's own literal command spelling (`bodger recurring skip`, not
// `bodger recurring occurrences skip`).
func newRecurringSkipCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "skip <occurrence-id>",
		Short: "Record that a pending occurrence's firing was deliberately not recorded",
		Long:  "Record that a pending occurrence's firing was deliberately not recorded. No transaction is created - money never moved and never will for this occurrence's date.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.SkipOccurrence(ctx, app.SkipOccurrenceCommand{ActorID: ports.SeededUserID, OccurrenceID: args[0]})
			if err != nil {
				return err
			}
			txID, _ := result.Occurrence.TransactionID()
			view := scheduledOccurrenceView{
				ID: result.Occurrence.ID(), RuleID: result.Occurrence.RuleID(),
				OccurrenceDate: result.Occurrence.OccurrenceDate().String(),
				Status:         string(result.Occurrence.Status()), TransactionID: txID,
			}
			return render(cmd, view, func(w io.Writer) { printScheduledOccurrence(w, view) })
		},
	}
}

// ---- recurring forecast ----

// forecastPointView is one period's projected totals (app.ForecastPoint),
// mirroring internal/surface/http/recurring.go's own forecastPointView.
type forecastPointView struct {
	From             string `json:"from"`
	To               string `json:"to"`
	ProjectedInflow  string `json:"projected_inflow"`
	ProjectedOutflow string `json:"projected_outflow"`
	ProjectedNet     string `json:"projected_net"`
}

// unconvertedOccurrenceView names one pending occurrence a forecast's
// conversion couldn't cover (app.UnconvertedOccurrence).
type unconvertedOccurrenceView struct {
	OccurrenceID string `json:"occurrence_id"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
	Reason       string `json:"reason"`
}

type forecastView struct {
	Currency    string                      `json:"currency"`
	Points      []forecastPointView         `json:"points"`
	Unconverted []unconvertedOccurrenceView `json:"unconverted,omitempty"`
}

func forecastViewFrom(r app.ForecastResult) forecastView {
	v := forecastView{Currency: r.Options.ReportingCurrency, Points: []forecastPointView{}}
	for _, p := range r.Points {
		v.Points = append(v.Points, forecastPointView{
			From: p.From.String(), To: p.To.String(),
			ProjectedInflow: p.ProjectedInflow.AmountString(), ProjectedOutflow: p.ProjectedOutflow.AmountString(),
			ProjectedNet: p.ProjectedNet.AmountString(),
		})
	}
	for _, u := range r.Unconverted {
		v.Unconverted = append(v.Unconverted, unconvertedOccurrenceView{
			OccurrenceID: u.OccurrenceID, Amount: u.Amount.AmountString(), Currency: u.Amount.Currency(), Reason: u.Reason,
		})
	}
	return v
}

func printForecast(w io.Writer, v forecastView) {
	if len(v.Points) == 0 {
		_, _ = fmt.Fprintln(w, "No pending occurrences in range.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "FROM\tTO\tPROJECTED INFLOW\tPROJECTED OUTFLOW\tPROJECTED NET")
	for _, p := range v.Points {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s %s\t%s %s\t%s %s\n",
			p.From, p.To, p.ProjectedInflow, v.Currency, p.ProjectedOutflow, v.Currency, p.ProjectedNet, v.Currency)
	}
	_ = tw.Flush()
	if len(v.Unconverted) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Couldn't convert (fetch the missing rate with `bodger fx rates fetch`):")
	for _, u := range v.Unconverted {
		_, _ = fmt.Fprintf(w, "  %s (%s %s): %s\n", u.OccurrenceID, u.Amount, u.Currency, u.Reason)
	}
}

func newRecurringForecastCmd(factory ServiceFactory) *cobra.Command {
	var from, to string
	var opts *reportOptionsFlags
	var granularity *string
	cmd := &cobra.Command{
		Use:   "forecast",
		Short: "See projected inflow/outflow/net from pending scheduled occurrences",
		Long: "See projected inflow/outflow/net from pending scheduled occurrences, bucketed by " +
			"period. Reads only pending occurrences - materialised and skipped occurrences are " +
			"resolved history, not a forecast - and never touches a balance, budget actual, or any " +
			"other actuals-only figure.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.Forecast(ctx, app.ForecastQuery{
				ActorID: ports.SeededUserID, FromDate: from, ToDate: to,
				Options: opts.options(), Granularity: app.Granularity(*granularity),
			})
			if err != nil {
				return err
			}
			view := forecastViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printForecast(w, view) })
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "the inclusive start of the forecast range (required)")
	cmd.Flags().StringVar(&to, "to", "", "the inclusive end of the forecast range (required)")
	opts = bindReportOptionsFlags(cmd)
	granularity = bindGranularityFlag(cmd)
	return cmd
}

// ---- command group ----

// newRecurringCmd builds the "recurring" command group: create, update,
// archive, list, occurrences (a "list" subgroup), materialise, skip, and
// forecast — one subcommand per use case internal/app's recurring rule,
// occurrence, and forecast files expose (issues #277-#280), per issue
// #281's own surface-wiring scope.
func newRecurringCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recurring",
		Short: "Manage recurring rules and their scheduled occurrences",
	}
	cmd.AddCommand(
		newRecurringCreateCmd(factory),
		newRecurringUpdateCmd(factory),
		newRecurringArchiveCmd(factory),
		newRecurringListCmd(factory),
		newRecurringOccurrencesCmd(factory),
		newRecurringMaterialiseCmd(factory),
		newRecurringSkipCmd(factory),
		newRecurringForecastCmd(factory),
	)
	return cmd
}
