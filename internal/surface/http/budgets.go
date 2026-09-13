// This file implements issue #244's REST surface for budgets, wired onto
// internal/app/budgets.go's CRUD use cases (issue #242) and
// internal/app/budget_actuals.go's actual-vs-budget queries (issue #243).
package http

import (
	"net/http"
	"strconv"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// budgetLineView is one BudgetLine's wire shape. amount is rendered via
// app.BudgetLineAmount, since a BudgetLine carries no currency of its own
// (its doc comment) and this package deliberately never imports
// internal/domain/money to pair one in directly (http.go's own doc
// comment).
type budgetLineView struct {
	ID         string `json:"id"`
	CategoryID string `json:"category_id"`
	Amount     string `json:"amount" format:"money"`
	Rollover   bool   `json:"rollover"`
}

// budgetView is a Budget's wire shape, lines included — there is no
// separate endpoint for "just the lines" of a budget, the same one-
// aggregate shape internal/app/budgets.go's own BudgetResult uses.
type budgetView struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	PeriodType string           `json:"period_type" enum:"budget_period_type"`
	Currency   string           `json:"currency"`
	StartsOn   string           `json:"starts_on" format:"date"`
	Archived   bool             `json:"archived"`
	ArchivedAt string           `json:"archived_at,omitempty" doc:"Set only once the budget is archived." format:"date"`
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

// budgetLineRequest is one line of createBudgetRequest's optional initial
// "lines" array, or addBudgetLineRequest's own body — the same
// category_ref/amount/rollover shape either way.
type budgetLineRequest struct {
	CategoryRef string `json:"category_ref" doc:"The category this line plans an amount for, by ID or unique name."`
	Amount      string `json:"amount" format:"money" doc:"The planned amount, in the budget's own currency."`
	Rollover    bool   `json:"rollover,omitempty" doc:"Carry an unspent (or overspent) amount into the next period."`
}

func budgetLineInputsFrom(reqs []budgetLineRequest) []app.BudgetLineInput {
	inputs := make([]app.BudgetLineInput, 0, len(reqs))
	for _, r := range reqs {
		inputs = append(inputs, app.BudgetLineInput{CategoryRef: r.CategoryRef, Amount: r.Amount, Rollover: r.Rollover})
	}
	return inputs
}

// createBudgetRequest is POST /api/v1/budgets' request body. period_type is
// absent: PeriodType is always monthly today (budgeting.PeriodTypeMonthly's
// own doc comment), so there's nothing for a caller to choose yet.
type createBudgetRequest struct {
	Name     string              `json:"name"`
	Currency string              `json:"currency,omitempty" doc:"Defaults to your reporting currency, then your instance default."`
	StartsOn string              `json:"starts_on,omitempty" doc:"The date the budget's periods are computed from. Defaults to today." format:"date"`
	Lines    []budgetLineRequest `json:"lines,omitempty" doc:"An initial batch of lines to create alongside the budget. Optional — lines can also be added afterward via POST .../lines."`
}

func (h *handlers) createBudget(w http.ResponseWriter, r *http.Request) {
	var body createBudgetRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	result, err := h.svc.CreateBudget(r.Context(), app.CreateBudgetCommand{
		ActorID:  actorID(r),
		Name:     body.Name,
		Currency: body.Currency,
		StartsOn: body.StartsOn,
		Lines:    budgetLineInputsFrom(body.Lines),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	view, err := budgetViewFrom(result)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusCreated, view)
}

func (h *handlers) listBudgets(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ListBudgets(r.Context(), app.ListBudgetsQuery{ActorID: actorID(r)})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	views := make([]budgetView, 0, len(result.Budgets))
	for _, b := range result.Budgets {
		v, err := budgetViewFrom(app.BudgetResult{Budget: b})
		if err != nil {
			h.respondError(w, r, err)
			return
		}
		views = append(views, v)
	}
	respond(w, http.StatusOK, views)
}

func (h *handlers) getBudget(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.GetBudget(r.Context(), app.GetBudgetQuery{ActorID: actorID(r), BudgetID: r.PathValue("id")})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	view, err := budgetViewFrom(result)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, view)
}

// patchBudgetRequest is PATCH /api/v1/budgets/{id}'s request body: a full
// replacement of both editable fields, the same "send every field's
// current value, not only the one that's changing" contract
// editTransactionRequest uses, rather than accounts' mutually-exclusive-
// fields shape — internal/app.UpdateBudgetCommand always sets both Name
// and StartsOn together, with no "leave this field alone" option: omitting
// starts_on resolves it to today, not to the budget's existing value.
type patchBudgetRequest struct {
	Name     string `json:"name"`
	StartsOn string `json:"starts_on,omitempty" doc:"Defaults to today when omitted - not to the budget's existing starts_on." format:"date"`
}

func (h *handlers) patchBudget(w http.ResponseWriter, r *http.Request) {
	var body patchBudgetRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	result, err := h.svc.UpdateBudget(r.Context(), app.UpdateBudgetCommand{
		ActorID: actorID(r), BudgetID: r.PathValue("id"), Name: body.Name, StartsOn: body.StartsOn,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	view, err := budgetViewFrom(result)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, view)
}

func (h *handlers) archiveBudget(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ArchiveBudget(r.Context(), app.ArchiveBudgetCommand{ActorID: actorID(r), BudgetID: r.PathValue("id")})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	view, err := budgetViewFrom(result)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, view)
}

func (h *handlers) addBudgetLine(w http.ResponseWriter, r *http.Request) {
	var body budgetLineRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	result, err := h.svc.AddBudgetLine(r.Context(), app.AddBudgetLineCommand{
		ActorID: actorID(r), BudgetID: r.PathValue("id"),
		CategoryRef: body.CategoryRef, Amount: body.Amount, Rollover: body.Rollover,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	view, err := budgetViewFrom(result)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusCreated, view)
}

// patchBudgetLineRequest is PATCH .../lines/{lineId}'s request body: a
// line's category is fixed once created (internal/app.UpdateBudgetLineCommand's
// own doc comment), so only amount and rollover are editable here.
type patchBudgetLineRequest struct {
	Amount   string `json:"amount" format:"money"`
	Rollover bool   `json:"rollover,omitempty"`
}

func (h *handlers) patchBudgetLine(w http.ResponseWriter, r *http.Request) {
	var body patchBudgetLineRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	result, err := h.svc.UpdateBudgetLine(r.Context(), app.UpdateBudgetLineCommand{
		ActorID: actorID(r), BudgetID: r.PathValue("id"), LineID: r.PathValue("lineId"),
		Amount: body.Amount, Rollover: body.Rollover,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	view, err := budgetViewFrom(result)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, view)
}

func (h *handlers) removeBudgetLine(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.RemoveBudgetLine(r.Context(), app.RemoveBudgetLineCommand{
		ActorID: actorID(r), BudgetID: r.PathValue("id"), LineID: r.PathValue("lineId"),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	view, err := budgetViewFrom(result)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, view)
}

// budgetLineActualsView is one line's plan-vs-actual for the period a
// budgetActualsView covers (app.BudgetLineActuals).
type budgetLineActualsView struct {
	LineID      string  `json:"line_id"`
	CategoryID  string  `json:"category_id"`
	Budgeted    string  `json:"budgeted" format:"money"`
	Actual      string  `json:"actual" format:"money"`
	Remaining   string  `json:"remaining" format:"money"`
	Utilisation float64 `json:"utilisation" doc:"Actual / Budgeted. Zero when Budgeted is zero."`
}

// budgetActualsView is GET .../actuals' response shape (app.BudgetActualsResult).
type budgetActualsView struct {
	BudgetID    string                   `json:"budget_id"`
	Currency    string                   `json:"currency"`
	From        string                   `json:"from" format:"date"`
	To          string                   `json:"to" format:"date"`
	Lines       []budgetLineActualsView  `json:"lines"`
	Unconverted []unconvertedPostingView `json:"unconverted,omitempty"`
}

func budgetActualsViewFrom(r app.BudgetActualsResult) budgetActualsView {
	v := budgetActualsView{
		BudgetID:    r.Budget.ID(),
		Currency:    r.Budget.Currency(),
		From:        r.From.String(),
		To:          r.To.String(),
		Lines:       []budgetLineActualsView{},
		Unconverted: unconvertedPostingViewsFrom(r.Unconverted),
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

func (h *handlers) getBudgetActuals(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.BudgetActuals(r.Context(), app.BudgetActualsQuery{
		ActorID: actorID(r), BudgetID: r.PathValue("id"), Period: r.URL.Query().Get("period"),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, budgetActualsViewFrom(result))
}

// budgetHistoryView is GET .../history's response shape
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

func (h *handlers) getBudgetHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	months := 0
	if raw := q.Get("months"); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil {
			h.respondError(w, r, errs.New(errs.InvalidInput).Explain("%q isn't a valid number of months.", raw).Field("months"))
			return
		}
		months = n
	}

	result, err := h.svc.BudgetHistory(r.Context(), app.BudgetHistoryQuery{
		ActorID: actorID(r), BudgetID: r.PathValue("id"), Period: q.Get("period"), Months: months,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, budgetHistoryViewFrom(result))
}
