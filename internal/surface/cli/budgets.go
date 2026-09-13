// This file implements issue #244's `bodger budgets` surface, wired onto
// internal/app/budgets.go's CRUD use cases (issue #242) and
// internal/app/budget_actuals.go's actual-vs-budget queries (issue #243)
// the same one-command, one-application-call way as every other command in
// this package.
package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// budgetLineView is budgets' counterpart to accounts.go's accountView —
// see its doc comment for why every field here is a plain primitive.
// Amount is rendered via app.BudgetLineAmount, since a BudgetLine carries
// no currency of its own (its doc comment) and this package may not import
// internal/domain/money to pair one in directly.
type budgetLineView struct {
	ID         string `json:"id"`
	CategoryID string `json:"category_id"`
	Amount     string `json:"amount"`
	Rollover   bool   `json:"rollover"`
}

// budgetView is `bodger budgets`' rendered shape for a Budget, lines
// included — a budget's lines are always fetched and shown alongside it
// (there is no separate "get just the lines" command), the same one-
// aggregate shape internal/app/budgets.go's BudgetResult itself uses.
type budgetView struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	PeriodType string           `json:"period_type"`
	Currency   string           `json:"currency"`
	StartsOn   string           `json:"starts_on"`
	Archived   bool             `json:"archived"`
	ArchivedAt string           `json:"archived_at,omitempty"`
	Lines      []budgetLineView `json:"lines"`
}

func budgetViewFrom(r app.BudgetResult) (budgetView, error) {
	b := r.Budget
	v := budgetView{
		ID:         b.ID(),
		Name:       b.Name(),
		PeriodType: string(b.PeriodType()),
		Currency:   b.Currency(),
		StartsOn:   b.StartsOn().String(),
		Archived:   b.Archived(),
		Lines:      []budgetLineView{},
	}
	if d, ok := b.ArchivedAt(); ok {
		v.ArchivedAt = d.String()
	}
	for _, l := range b.Lines() {
		amount, err := app.BudgetLineAmount(b.Currency(), l)
		if err != nil {
			return budgetView{}, err
		}
		v.Lines = append(v.Lines, budgetLineView{
			ID:         l.ID(),
			CategoryID: l.CategoryID(),
			Amount:     amount.AmountString(),
			Rollover:   l.Rollover(),
		})
	}
	return v, nil
}

func printBudget(w io.Writer, v budgetView) {
	archived := ""
	if v.Archived {
		archived = " — archived"
	}
	_, _ = fmt.Fprintf(w, "%s (%s, starts %s)%s\n", v.Name, v.Currency, v.StartsOn, archived)
	_, _ = fmt.Fprintf(w, "id: %s\n", v.ID)
	if len(v.Lines) == 0 {
		_, _ = fmt.Fprintln(w, "No lines yet. Add one with `bodger budgets lines add`.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "LINE ID\tCATEGORY ID\tAMOUNT\tROLLOVER")
	for _, l := range v.Lines {
		rollover := "no"
		if l.Rollover {
			rollover = "yes"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s %s\t%s\n", l.ID, l.CategoryID, l.Amount, v.Currency, rollover)
	}
	_ = tw.Flush()
}

func printBudgetTable(w io.Writer, views []budgetView) {
	if len(views) == 0 {
		_, _ = fmt.Fprintln(w, "No budgets yet. Add one with `bodger budgets add`.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tCURRENCY\tSTARTS ON\tLINES\tARCHIVED")
	for _, v := range views {
		archived := "no"
		if v.Archived {
			archived = "yes"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", v.Name, v.Currency, v.StartsOn, len(v.Lines), archived)
	}
	_ = tw.Flush()
}

// budgetLineActualsView is one line's plan-vs-actual for the period a
// budgetActualsView covers (app.BudgetLineActuals).
type budgetLineActualsView struct {
	LineID      string  `json:"line_id"`
	CategoryID  string  `json:"category_id"`
	Budgeted    string  `json:"budgeted"`
	Actual      string  `json:"actual"`
	Remaining   string  `json:"remaining"`
	Utilisation float64 `json:"utilisation"`
}

// budgetOverallActualsView is every line's plan-vs-actual summed into one
// figure for the whole budget (app.BudgetOverallActuals) — the same shape
// budgetLineActualsView reports per line, minus the fields that only make
// sense for a single line.
type budgetOverallActualsView struct {
	Budgeted    string  `json:"budgeted"`
	Actual      string  `json:"actual"`
	Remaining   string  `json:"remaining"`
	Utilisation float64 `json:"utilisation"`
}

// budgetActualsView is `bodger budgets actuals`' rendered shape
// (app.BudgetActualsResult). Unconverted mirrors balance.go's own
// unconvertedBalanceView convention, but keyed by transaction rather than
// account — a budget line's actual can pool contributions from more than
// one posting, unlike a single account's balance.
type budgetActualsView struct {
	BudgetID string `json:"budget_id"`
	Currency string `json:"currency"`
	From     string `json:"from"`
	To       string `json:"to"`
	// AsOf mirrors balance.go's own AsOf field — today, in your configured
	// timezone, so a caller can tell whether this period is the current
	// one, a past one, or a future one without computing "today" itself.
	AsOf        string                      `json:"as_of"`
	Overall     budgetOverallActualsView    `json:"overall"`
	Lines       []budgetLineActualsView     `json:"lines"`
	Unconverted []unconvertedPostingCLIView `json:"unconverted,omitempty"`
}

// unconvertedPostingCLIView names one posting a budget actuals/history
// query's conversion couldn't cover (app.UnconvertedPosting) — listed
// plainly with its reason, the same "not silently dropped" rule
// unconvertedBalanceView already follows.
type unconvertedPostingCLIView struct {
	TransactionID string `json:"transaction_id"`
	Amount        string `json:"amount"`
	Currency      string `json:"currency"`
	Reason        string `json:"reason"`
}

func unconvertedPostingCLIViewsFrom(u []app.UnconvertedPosting) []unconvertedPostingCLIView {
	views := make([]unconvertedPostingCLIView, 0, len(u))
	for _, p := range u {
		views = append(views, unconvertedPostingCLIView{
			TransactionID: p.TransactionID,
			Amount:        p.Amount.AmountString(),
			Currency:      p.Amount.Currency(),
			Reason:        p.Reason,
		})
	}
	return views
}

func budgetActualsViewFrom(r app.BudgetActualsResult) budgetActualsView {
	v := budgetActualsView{
		BudgetID: r.Budget.ID(),
		Currency: r.Budget.Currency(),
		From:     r.From.String(),
		To:       r.To.String(),
		AsOf:     r.AsOf.String(),
		Overall: budgetOverallActualsView{
			Budgeted:    r.Overall.Budgeted.AmountString(),
			Actual:      r.Overall.Actual.AmountString(),
			Remaining:   r.Overall.Remaining.AmountString(),
			Utilisation: r.Overall.Utilisation,
		},
		Lines:       []budgetLineActualsView{},
		Unconverted: unconvertedPostingCLIViewsFrom(r.Unconverted),
	}
	for _, l := range r.Lines {
		v.Lines = append(v.Lines, budgetLineActualsView{
			LineID:      l.Line.ID(),
			CategoryID:  l.Line.CategoryID(),
			Budgeted:    l.Budgeted.AmountString(),
			Actual:      l.Actual.AmountString(),
			Remaining:   l.Remaining.AmountString(),
			Utilisation: l.Utilisation,
		})
	}
	return v
}

func printBudgetActuals(w io.Writer, v budgetActualsView) {
	_, _ = fmt.Fprintf(w, "Period %s to %s (as of %s):\n", v.From, v.To, v.AsOf)
	_, _ = fmt.Fprintf(w, "Overall: %s / %s %s (%.0f%%)\n",
		v.Overall.Actual, v.Overall.Budgeted, v.Currency, v.Overall.Utilisation*100)
	if len(v.Lines) == 0 {
		_, _ = fmt.Fprintln(w, "No lines to report.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "CATEGORY ID\tBUDGETED\tACTUAL\tREMAINING\tUTILISATION")
	for _, l := range v.Lines {
		_, _ = fmt.Fprintf(tw, "%s\t%s %s\t%s %s\t%s %s\t%.0f%%\n",
			l.CategoryID, l.Budgeted, v.Currency, l.Actual, v.Currency, l.Remaining, v.Currency, l.Utilisation*100)
	}
	_ = tw.Flush()
	printUnconvertedPostings(w, v.Unconverted)
}

func printUnconvertedPostings(w io.Writer, u []unconvertedPostingCLIView) {
	if len(u) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Couldn't convert (fetch the missing rate with `bodger fx rates fetch`):")
	for _, p := range u {
		_, _ = fmt.Fprintf(w, "  %s (%s %s): %s\n", p.TransactionID, p.Amount, p.Currency, p.Reason)
	}
}

// budgetHistoryView is `bodger budgets history`'s rendered shape
// (app.BudgetHistoryResult): one budgetActualsView per month, oldest first.
type budgetHistoryView struct {
	BudgetID string              `json:"budget_id"`
	Periods  []budgetActualsView `json:"periods"`
}

func budgetHistoryViewFrom(r app.BudgetHistoryResult) budgetHistoryView {
	v := budgetHistoryView{BudgetID: r.Budget.ID(), Periods: []budgetActualsView{}}
	for _, p := range r.Periods {
		v.Periods = append(v.Periods, budgetActualsViewFrom(p))
	}
	return v
}

func printBudgetHistory(w io.Writer, v budgetHistoryView) {
	if len(v.Periods) == 0 {
		_, _ = fmt.Fprintln(w, "No periods to report — the budget hasn't started yet.")
		return
	}
	for i, p := range v.Periods {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		printBudgetActuals(w, p)
	}
}

// newBudgetsCmd builds the "budgets" command group: list, add, show,
// update, archive, actuals, and history — one subcommand per use case
// internal/app/budgets.go and internal/app/budget_actuals.go expose —
// plus a "lines" subgroup for managing a budget's lines.
func newBudgetsCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "budgets",
		Short: "Plan and track spending against a category",
	}
	cmd.AddCommand(
		newBudgetsListCmd(factory),
		newBudgetsShowCmd(factory),
		newBudgetsAddCmd(factory),
		newBudgetsUpdateCmd(factory),
		newBudgetsArchiveCmd(factory),
		newBudgetsActualsCmd(factory),
		newBudgetsHistoryCmd(factory),
		newBudgetsLinesCmd(factory),
	)
	return cmd
}

func newBudgetsListCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your budgets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ListBudgets(ctx, app.ListBudgetsQuery{ActorID: ports.SeededUserID})
			if err != nil {
				return err
			}
			views := make([]budgetView, len(result.Budgets))
			for i, b := range result.Budgets {
				v, err := budgetViewFrom(app.BudgetResult{Budget: b})
				if err != nil {
					return err
				}
				views[i] = v
			}
			return render(cmd, views, func(w io.Writer) { printBudgetTable(w, views) })
		},
	}
}

// newBudgetsShowCmd builds "budgets show". A budget is looked up by ID
// only (see internal/app/budgets.go's UpdateBudgetCommand doc comment for
// why: unlike an account or category, a budget's Name carries no
// uniqueness constraint to resolve a ref against).
func newBudgetsShowCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "show <budget-id>",
		Short: "Show one budget and its lines",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.GetBudget(ctx, app.GetBudgetQuery{ActorID: ports.SeededUserID, BudgetID: args[0]})
			if err != nil {
				return err
			}
			view, err := budgetViewFrom(result)
			if err != nil {
				return err
			}
			return render(cmd, view, func(w io.Writer) { printBudget(w, view) })
		},
	}
}

type budgetAddFlags struct {
	currency string
	startsOn string
}

// newBudgetsAddCmd builds "budgets add". It creates an empty budget —
// lines are added afterward with `bodger budgets lines add`, one at a
// time, the same "one thing per command" shape every other multi-step CLI
// resource in this package follows (e.g. accounts, then transactions
// against them) — even though internal/app.CreateBudgetCommand itself
// also accepts an initial batch of lines for a caller (like the HTTP
// surface's request body) that can express them more naturally than a
// repeated CLI flag could.
func newBudgetsAddCmd(factory ServiceFactory) *cobra.Command {
	var f budgetAddFlags
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a new budget",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.CreateBudget(ctx, app.CreateBudgetCommand{
				ActorID:  ports.SeededUserID,
				Name:     args[0],
				Currency: f.currency,
				StartsOn: f.startsOn,
			})
			if err != nil {
				return err
			}
			view, err := budgetViewFrom(result)
			if err != nil {
				return err
			}
			return render(cmd, view, func(w io.Writer) { printBudget(w, view) })
		},
	}
	cmd.Flags().StringVar(&f.currency, "currency", "", "the budget's currency code (defaults to your reporting currency, then your instance default)")
	cmd.Flags().StringVar(&f.startsOn, "starts-on", "", "the date the budget's periods are computed from (defaults to today)")
	return cmd
}

// newBudgetsUpdateCmd builds "budgets update". Both <name> and --starts-on
// are always set together in one call, mirroring `editTransaction`'s full-
// replacement contract rather than accounts' mutually-exclusive-fields
// PATCH: internal/app.UpdateBudgetCommand has no "leave this field alone"
// option, so omitting --starts-on resolves it to today (the same empty-
// means-today convention `budgets add --starts-on` uses), not to the
// budget's existing value — spelled out here so this doesn't surprise a
// caller who only meant to rename.
func newBudgetsUpdateCmd(factory ServiceFactory) *cobra.Command {
	var startsOn string
	cmd := &cobra.Command{
		Use:   "update <budget-id> <name>",
		Short: "Rename a budget and/or set its starts-on date",
		Long: "Rename a budget and/or set its starts-on date.\n\n" +
			"Both fields are always set together: if you omit --starts-on, it resolves to " +
			"today, not to the budget's existing starts-on date. Pass the budget's current " +
			"--starts-on back explicitly if you only mean to rename it.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.UpdateBudget(ctx, app.UpdateBudgetCommand{
				ActorID:  ports.SeededUserID,
				BudgetID: args[0],
				Name:     args[1],
				StartsOn: startsOn,
			})
			if err != nil {
				return err
			}
			view, err := budgetViewFrom(result)
			if err != nil {
				return err
			}
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Updated budget %q (starts %s).\n", view.Name, view.StartsOn)
			})
		},
	}
	cmd.Flags().StringVar(&startsOn, "starts-on", "", "the date the budget's periods are computed from (defaults to today)")
	return cmd
}

func newBudgetsArchiveCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <budget-id>",
		Short: "Archive a budget, hiding it from listings while keeping its history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ArchiveBudget(ctx, app.ArchiveBudgetCommand{ActorID: ports.SeededUserID, BudgetID: args[0]})
			if err != nil {
				return err
			}
			view, err := budgetViewFrom(result)
			if err != nil {
				return err
			}
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Archived budget %q.\n", view.Name)
			})
		},
	}
}

// newBudgetsActualsCmd builds "budgets actuals": issue #243's single-period
// use case. --period accepts any date within the target month and defaults
// to the current month, per BudgetActualsQuery's own doc comment.
func newBudgetsActualsCmd(factory ServiceFactory) *cobra.Command {
	var period string
	cmd := &cobra.Command{
		Use:   "actuals <budget-id>",
		Short: "See actual spend against a budget's lines for a period",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.BudgetActuals(ctx, app.BudgetActualsQuery{
				ActorID: ports.SeededUserID, BudgetID: args[0], Period: period,
			})
			if err != nil {
				return err
			}
			view := budgetActualsViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printBudgetActuals(w, view) })
		},
	}
	cmd.Flags().StringVar(&period, "period", "", "any date within the target month (defaults to the current month)")
	return cmd
}

// newBudgetsHistoryCmd builds "budgets history": issue #243's history use
// case, a repeated actuals computation over --months consecutive calendar
// months ending at --period's month.
func newBudgetsHistoryCmd(factory ServiceFactory) *cobra.Command {
	var period string
	var months int
	cmd := &cobra.Command{
		Use:   "history <budget-id>",
		Short: "See actual-vs-budget over several months",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.BudgetHistory(ctx, app.BudgetHistoryQuery{
				ActorID: ports.SeededUserID, BudgetID: args[0], Period: period, Months: months,
			})
			if err != nil {
				return err
			}
			view := budgetHistoryViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printBudgetHistory(w, view) })
		},
	}
	cmd.Flags().StringVar(&period, "period", "", "any date within the most recent month to include (defaults to the current month)")
	cmd.Flags().IntVar(&months, "months", 0, "how many consecutive months to include (defaults to 6)")
	return cmd
}

// newBudgetsLinesCmd builds the "budgets lines" subgroup: add, update, and
// remove, one per use case internal/app/budgets.go exposes for a budget's
// lines.
func newBudgetsLinesCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lines",
		Short: "Manage a budget's lines",
	}
	cmd.AddCommand(
		newBudgetsLinesAddCmd(factory),
		newBudgetsLinesUpdateCmd(factory),
		newBudgetsLinesRemoveCmd(factory),
	)
	return cmd
}

func newBudgetsLinesAddCmd(factory ServiceFactory) *cobra.Command {
	var rollover bool
	cmd := &cobra.Command{
		Use:   "add <budget-id> <category> <amount>",
		Short: "Add a line to a budget",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.AddBudgetLine(ctx, app.AddBudgetLineCommand{
				ActorID:     ports.SeededUserID,
				BudgetID:    args[0],
				CategoryRef: args[1],
				Amount:      args[2],
				Rollover:    rollover,
			})
			if err != nil {
				return err
			}
			view, err := budgetViewFrom(result)
			if err != nil {
				return err
			}
			return render(cmd, view, func(w io.Writer) { printBudget(w, view) })
		},
	}
	cmd.Flags().BoolVar(&rollover, "rollover", false, "carry an unspent (or overspent) amount into the next period")
	return cmd
}

func newBudgetsLinesUpdateCmd(factory ServiceFactory) *cobra.Command {
	var rollover bool
	cmd := &cobra.Command{
		Use:   "update <budget-id> <line-id> <amount>",
		Short: "Update a budget line's amount and rollover flag",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.UpdateBudgetLine(ctx, app.UpdateBudgetLineCommand{
				ActorID:  ports.SeededUserID,
				BudgetID: args[0],
				LineID:   args[1],
				Amount:   args[2],
				Rollover: rollover,
			})
			if err != nil {
				return err
			}
			view, err := budgetViewFrom(result)
			if err != nil {
				return err
			}
			return render(cmd, view, func(w io.Writer) { printBudget(w, view) })
		},
	}
	cmd.Flags().BoolVar(&rollover, "rollover", false, "carry an unspent (or overspent) amount into the next period")
	return cmd
}

func newBudgetsLinesRemoveCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <budget-id> <line-id>",
		Short: "Remove a line from a budget",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.RemoveBudgetLine(ctx, app.RemoveBudgetLineCommand{
				ActorID: ports.SeededUserID, BudgetID: args[0], LineID: args[1],
			})
			if err != nil {
				return err
			}
			view, err := budgetViewFrom(result)
			if err != nil {
				return err
			}
			return render(cmd, view, func(w io.Writer) { printBudget(w, view) })
		},
	}
}
