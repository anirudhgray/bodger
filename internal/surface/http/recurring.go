// This file implements issue #281's REST surface for recurring rules and
// scheduled occurrences, wired onto internal/app/recurring_rules.go's CRUD
// use cases (issue #277), internal/app/recurring_occurrences.go's
// generation and ListScheduledOccurrences query (issue #278, extended by
// #281 itself), internal/app/recurring_occurrences_actions.go's
// materialise/skip use cases (issue #279), and internal/app/forecast.go's
// projected-activity query (issue #280) — following
// internal/surface/http/budgets.go's own view/request/handler conventions.
package http

import (
	"net/http"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
)

// scheduleView is a RecurringRule's firing pattern, mirroring
// internal/app/export_json.go's jsonRecurringRule's own schedule fields —
// only the fields the declared frequency actually uses are populated,
// the same "only the right subset is ever read" discipline
// recurring.Schedule's own accessors enforce.
type scheduleView struct {
	Frequency  string `json:"frequency" enum:"recurring_frequency"`
	Interval   int    `json:"interval"`
	Weekday    *int   `json:"weekday,omitempty" doc:"0 (Sunday) - 6 (Saturday). Set only for a weekly schedule." format:"int32"`
	DayOfMonth *int   `json:"day_of_month,omitempty" doc:"1-31. Set for a monthly or yearly schedule." format:"int32"`
	Month      *int   `json:"month,omitempty" doc:"1 (January) - 12 (December). Set only for a yearly schedule." format:"int32"`
}

// scheduleRequest is a recurring rule's schedule as sent by a caller —
// createRecurringRuleRequest's and patchRecurringRuleRequest's own
// "schedule" field. Every field is always sent; buildSchedule
// (internal/app/recurring_rules.go) reads only the subset its Frequency
// actually needs, ignoring the rest — the same convention
// RecurringScheduleInput's own doc comment describes.
type scheduleRequest struct {
	Frequency  string `json:"frequency" enum:"recurring_frequency"`
	Interval   int    `json:"interval"`
	Weekday    int    `json:"weekday,omitempty" doc:"0 (Sunday) - 6 (Saturday). Required for a weekly schedule."`
	DayOfMonth int    `json:"day_of_month,omitempty" doc:"1-31. Required for a monthly or yearly schedule."`
	Month      int    `json:"month,omitempty" doc:"1 (January) - 12 (December). Required for a yearly schedule."`
}

func (s scheduleRequest) input() app.RecurringScheduleInput {
	return app.RecurringScheduleInput{
		Frequency:  s.Frequency,
		Interval:   s.Interval,
		Weekday:    time.Weekday(s.Weekday),
		DayOfMonth: s.DayOfMonth,
		Month:      time.Month(s.Month),
	}
}

// recurringRuleView is a RecurringRule's wire shape. Amount is rendered
// via app.RecurringRuleAmount, since a RecurringRule carries no currency
// of its own (its own doc comment) and this package deliberately never
// imports internal/domain/money to pair one in directly (http.go's own
// doc comment) — the same reasoning budgetView's own Amount field
// follows.
type recurringRuleView struct {
	ID          string       `json:"id"`
	AccountID   string       `json:"account_id"`
	CategoryID  string       `json:"category_id"`
	Amount      string       `json:"amount" format:"money"`
	Currency    string       `json:"currency"`
	Description string       `json:"description"`
	Schedule    scheduleView `json:"schedule"`
	StartsOn    string       `json:"starts_on" format:"date"`
	EndsOn      string       `json:"ends_on,omitempty" doc:"Absent when the rule runs indefinitely." format:"date"`
	Archived    bool         `json:"archived"`
	ArchivedAt  string       `json:"archived_at,omitempty" doc:"Set only once the rule is archived." format:"date"`
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

// createRecurringRuleRequest is POST /api/v1/recurring-rules' request
// body.
type createRecurringRuleRequest struct {
	AccountRef  string          `json:"account_ref" doc:"The account a materialised occurrence would post to: an account's ID or unique name."`
	CategoryRef string          `json:"category_ref" doc:"The category a materialised occurrence would be attributed to, which also decides its direction: an income category makes this an inflow, an expense category an outflow."`
	Amount      string          `json:"amount" format:"money" doc:"The planned amount, in the account's own currency. Always positive."`
	Description string          `json:"description"`
	Schedule    scheduleRequest `json:"schedule"`
	StartsOn    string          `json:"starts_on,omitempty" doc:"The first date the rule may fire on. Defaults to today." format:"date"`
	EndsOn      string          `json:"ends_on,omitempty" doc:"The last date the rule may fire on. Omit for a rule that runs indefinitely." format:"date"`
}

func (h *handlers) createRecurringRule(w http.ResponseWriter, r *http.Request) {
	var body createRecurringRuleRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	result, err := h.svc.CreateRecurringRule(r.Context(), app.CreateRecurringRuleCommand{
		ActorID:     actorID(r),
		AccountRef:  body.AccountRef,
		CategoryRef: body.CategoryRef,
		Amount:      body.Amount,
		Description: body.Description,
		Schedule:    body.Schedule.input(),
		StartsOn:    body.StartsOn,
		EndsOn:      body.EndsOn,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	view, err := recurringRuleViewFrom(result)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusCreated, view)
}

func (h *handlers) listRecurringRules(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ListRecurringRules(r.Context(), app.ListRecurringRulesQuery{ActorID: actorID(r)})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	views := make([]recurringRuleView, 0, len(result.Rules))
	for _, rule := range result.Rules {
		view, err := recurringRuleViewFrom(app.RecurringRuleResult{Rule: rule, Currency: result.Currencies[rule.ID()]})
		if err != nil {
			h.respondError(w, r, err)
			return
		}
		views = append(views, view)
	}
	respond(w, http.StatusOK, views)
}

func (h *handlers) getRecurringRule(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.GetRecurringRule(r.Context(), app.GetRecurringRuleQuery{ActorID: actorID(r), RuleID: r.PathValue("id")})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	view, err := recurringRuleViewFrom(result)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, view)
}

// patchRecurringRuleRequest is PATCH /api/v1/recurring-rules/{id}'s
// request body: a full replacement of every editable field, the same
// contract patchBudgetRequest uses — internal/app.UpdateRecurringRuleCommand
// always sets amount, description, schedule, and ends_on together, with
// no "leave this field alone" option (its own doc comment). account_ref,
// category_ref, and starts_on aren't editable at all — see that same doc
// comment for why.
type patchRecurringRuleRequest struct {
	Amount      string          `json:"amount" format:"money"`
	Description string          `json:"description"`
	Schedule    scheduleRequest `json:"schedule"`
	EndsOn      string          `json:"ends_on,omitempty" doc:"Omitting this clears the rule to run indefinitely — it does not leave the existing ends_on untouched." format:"date"`
}

func (h *handlers) patchRecurringRule(w http.ResponseWriter, r *http.Request) {
	var body patchRecurringRuleRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, r, err)
		return
	}

	result, err := h.svc.UpdateRecurringRule(r.Context(), app.UpdateRecurringRuleCommand{
		ActorID:     actorID(r),
		RuleID:      r.PathValue("id"),
		Amount:      body.Amount,
		Description: body.Description,
		Schedule:    body.Schedule.input(),
		EndsOn:      body.EndsOn,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	view, err := recurringRuleViewFrom(result)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, view)
}

func (h *handlers) archiveRecurringRule(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ArchiveRecurringRule(r.Context(), app.ArchiveRecurringRuleCommand{ActorID: actorID(r), RuleID: r.PathValue("id")})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	view, err := recurringRuleViewFrom(result)
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, view)
}

// scheduledOccurrenceView is a ScheduledOccurrence's wire shape, mirroring
// internal/app/export_json.go's jsonScheduledOccurrence field for field —
// no account, no currency, no amount, since an occurrence is a
// projection, never money that moved (data-model.md §11).
type scheduledOccurrenceView struct {
	ID             string `json:"id"`
	RuleID         string `json:"rule_id"`
	OccurrenceDate string `json:"occurrence_date" format:"date"`
	Status         string `json:"status" enum:"occurrence_status"`
	TransactionID  string `json:"transaction_id,omitempty" doc:"Set only when status is \"materialised\"."`
}

func (h *handlers) listScheduledOccurrences(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.svc.ListScheduledOccurrences(r.Context(), app.ListScheduledOccurrencesQuery{
		ActorID:  actorID(r),
		RuleID:   q.Get("rule_id"),
		Status:   q.Get("status"),
		FromDate: q.Get("from"),
		ToDate:   q.Get("to"),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	views := make([]scheduledOccurrenceView, 0, len(result.Occurrences))
	for _, occ := range result.Occurrences {
		txID, _ := occ.TransactionID()
		views = append(views, scheduledOccurrenceView{
			ID:             occ.ID(),
			RuleID:         occ.RuleID(),
			OccurrenceDate: occ.OccurrenceDate().String(),
			Status:         string(occ.Status()),
			TransactionID:  txID,
		})
	}
	respond(w, http.StatusOK, views)
}

// materialiseOccurrenceView is POST .../materialise's response shape
// (app.MaterialiseOccurrenceResult): the transaction it produced, and the
// occurrence's own updated state.
type materialiseOccurrenceView struct {
	Occurrence  scheduledOccurrenceView `json:"occurrence"`
	Transaction transactionView         `json:"transaction"`
}

func (h *handlers) materialiseOccurrence(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.MaterialiseOccurrence(r.Context(), app.MaterialiseOccurrenceCommand{
		ActorID: actorID(r), OccurrenceID: r.PathValue("id"),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	txID, _ := result.Occurrence.TransactionID()
	respond(w, http.StatusOK, materialiseOccurrenceView{
		Occurrence: scheduledOccurrenceView{
			ID:             result.Occurrence.ID(),
			RuleID:         result.Occurrence.RuleID(),
			OccurrenceDate: result.Occurrence.OccurrenceDate().String(),
			Status:         string(result.Occurrence.Status()),
			TransactionID:  txID,
		},
		Transaction: transactionViewFrom(app.TransactionResult{Transaction: result.Transaction}),
	})
}

func (h *handlers) skipOccurrence(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.SkipOccurrence(r.Context(), app.SkipOccurrenceCommand{
		ActorID: actorID(r), OccurrenceID: r.PathValue("id"),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	txID, _ := result.Occurrence.TransactionID()
	respond(w, http.StatusOK, scheduledOccurrenceView{
		ID:             result.Occurrence.ID(),
		RuleID:         result.Occurrence.RuleID(),
		OccurrenceDate: result.Occurrence.OccurrenceDate().String(),
		Status:         string(result.Occurrence.Status()),
		TransactionID:  txID,
	})
}

// forecastPointView is one period's projected totals (app.ForecastPoint) —
// Projected* field names mirroring the app layer's own deliberate
// "never bare Inflow/Outflow/Net" guard against confusing this with a
// cashFlowPointView.
type forecastPointView struct {
	From             string `json:"from" format:"date"`
	To               string `json:"to" format:"date"`
	ProjectedInflow  string `json:"projected_inflow" format:"money"`
	ProjectedOutflow string `json:"projected_outflow" format:"money"`
	ProjectedNet     string `json:"projected_net" format:"money"`
}

// unconvertedOccurrenceView names one pending occurrence a forecast's
// conversion couldn't cover (app.UnconvertedOccurrence) — listed plainly
// with its reason, the same "not silently dropped" rule
// unconvertedPostingView already follows.
type unconvertedOccurrenceView struct {
	OccurrenceID string `json:"occurrence_id"`
	Amount       string `json:"amount" format:"money"`
	Currency     string `json:"currency"`
	Reason       string `json:"reason"`
}

// forecastView is GET /api/v1/forecast's response shape
// (app.ForecastResult).
type forecastView struct {
	Currency    string                      `json:"currency"`
	Points      []forecastPointView         `json:"points"`
	Unconverted []unconvertedOccurrenceView `json:"unconverted,omitempty"`
}

func forecastViewFrom(r app.ForecastResult) forecastView {
	v := forecastView{
		Currency: r.Options.ReportingCurrency,
		Points:   []forecastPointView{},
	}
	for _, p := range r.Points {
		v.Points = append(v.Points, forecastPointView{
			From:             p.From.String(),
			To:               p.To.String(),
			ProjectedInflow:  p.ProjectedInflow.AmountString(),
			ProjectedOutflow: p.ProjectedOutflow.AmountString(),
			ProjectedNet:     p.ProjectedNet.AmountString(),
		})
	}
	for _, u := range r.Unconverted {
		v.Unconverted = append(v.Unconverted, unconvertedOccurrenceView{
			OccurrenceID: u.OccurrenceID,
			Amount:       u.Amount.AmountString(),
			Currency:     u.Amount.Currency(),
			Reason:       u.Reason,
		})
	}
	return v
}

func (h *handlers) getForecast(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	result, err := h.svc.Forecast(r.Context(), app.ForecastQuery{
		ActorID:     actorID(r),
		FromDate:    q.Get("from"),
		ToDate:      q.Get("to"),
		Options:     parseReportOptionsQuery(q),
		Granularity: parseGranularityQuery(q),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	respond(w, http.StatusOK, forecastViewFrom(result))
}
