package http

import (
	"net/http"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

// createCategoryRequest is POST /api/v1/categories' request body. Parent,
// when non-empty, resolves against the actor's own categories the same
// forgiving way an account reference does (a UUID, or a unique name) —
// resolution happens entirely in the application layer.
type createCategoryRequest struct {
	Name      string `json:"name"`
	Type      string `json:"type" enum:"category_kind"`
	Parent    string `json:"parent,omitempty" doc:"A parent category's ID or unique name. Omit for a top-level category."`
	SortOrder int    `json:"sort_order,omitempty" doc:"Where the category sorts in a list; lower comes first."`
}

func (h *handlers) createCategory(w http.ResponseWriter, r *http.Request) {
	var body createCategoryRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, err)
		return
	}

	result, err := h.svc.CreateCategory(r.Context(), app.CreateCategoryCommand{
		ActorID:   actorID(r),
		Name:      body.Name,
		Kind:      body.Type,
		ParentRef: body.Parent,
		SortOrder: body.SortOrder,
	})
	if err != nil {
		h.respondError(w, err)
		return
	}
	respond(w, http.StatusCreated, categoryViewFrom(result))
}

func (h *handlers) listCategories(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ListCategories(r.Context(), app.ListCategoriesQuery{ActorID: actorID(r)})
	if err != nil {
		h.respondError(w, err)
		return
	}
	views := make([]categoryView, 0, len(result.Categories))
	for _, c := range result.Categories {
		views = append(views, categoryViewFrom(app.CategoryResult{Category: c}))
	}
	respond(w, http.StatusOK, views)
}

func (h *handlers) getCategory(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.GetCategory(r.Context(), app.GetCategoryQuery{
		ActorID:     actorID(r),
		CategoryRef: r.PathValue("id"),
	})
	if err != nil {
		h.respondError(w, err)
		return
	}
	respond(w, http.StatusOK, categoryViewFrom(result))
}

// patchCategoryRequest is PATCH /api/v1/categories/{id}'s request body —
// accounts.go's patchAccountRequest, for categories: exactly one of
// renaming or reparenting per request. Parent is a *string, not a plain
// string, because "" is itself meaningful (ReparentCategory's "move to
// top-level") and has to be distinguished from "not present in this
// request at all".
type patchCategoryRequest struct {
	Name   *string `json:"name,omitempty" doc:"The category's new name."`
	Parent *string `json:"parent,omitempty" doc:"A parent category's ID or unique name. An empty string moves the category to the top level."`
}

func (h *handlers) patchCategory(w http.ResponseWriter, r *http.Request) {
	var body patchCategoryRequest
	if err := decodeJSON(r, &body); err != nil {
		h.respondError(w, err)
		return
	}

	id := r.PathValue("id")
	switch {
	case body.Name != nil && body.Parent == nil:
		result, err := h.svc.RenameCategory(r.Context(), app.RenameCategoryCommand{
			ActorID: actorID(r), CategoryRef: id, Name: *body.Name,
		})
		if err != nil {
			h.respondError(w, err)
			return
		}
		respond(w, http.StatusOK, categoryViewFrom(result))

	case body.Parent != nil && body.Name == nil:
		result, err := h.svc.ReparentCategory(r.Context(), app.ReparentCategoryCommand{
			ActorID: actorID(r), CategoryRef: id, ParentRef: *body.Parent,
		})
		if err != nil {
			h.respondError(w, err)
			return
		}
		respond(w, http.StatusOK, categoryViewFrom(result))

	default:
		h.respondError(w, errs.New(errs.InvalidInput).
			Explain("A request must set exactly one of \"name\" or \"parent\"."))
	}
}

func (h *handlers) archiveCategory(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.ArchiveCategory(r.Context(), app.ArchiveCategoryCommand{
		ActorID: actorID(r), CategoryRef: r.PathValue("id"),
	})
	if err != nil {
		h.respondError(w, err)
		return
	}
	respond(w, http.StatusOK, categoryViewFrom(result))
}
