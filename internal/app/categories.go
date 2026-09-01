package app

import (
	"context"
	"strings"

	"github.com/anirudhgray/bodger/internal/app/normalize"
	"github.com/anirudhgray/bodger/internal/domain/ledger"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

const maxCategoryNameLen = 200

// CategoryResult wraps the Category a create/rename/reparent/archive use
// case produced or affected.
type CategoryResult struct {
	Category ledger.Category
}

// CreateCategoryCommand creates a new category. ParentRef, when non-empty,
// must resolve against one of the actor's own categories (self-parenting
// and top-level are both expressed differently: ParentRef == "" is
// top-level, never a self-reference). Deeper cycle detection is the
// repository's job (internal/adapters/sqlite's checkNoCycle) — nothing
// here can introduce one anyway, since a brand-new category has no
// descendants yet to have accidentally become its own ancestor.
type CreateCategoryCommand struct {
	ActorID   string
	Name      string
	Kind      string
	ParentRef string
	SortOrder int
}

// CreateCategory implements the "create" use case in issue #6's
// categories scope.
func (s *Service) CreateCategory(ctx context.Context, cmd CreateCategoryCommand) (CategoryResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return CategoryResult{}, err
	}

	name, err := normalize.Text(cmd.Name, maxCategoryNameLen)
	if err != nil {
		return CategoryResult{}, attachField(err, "name")
	}
	if name == "" {
		return CategoryResult{}, errs.New(errs.InvalidInput).Explain("A name is required.").Field("name")
	}

	kind, err := parseCategoryKind(cmd.Kind)
	if err != nil {
		return CategoryResult{}, err
	}

	var parentIDPtr *string
	if strings.TrimSpace(cmd.ParentRef) != "" {
		parent, err := s.resolveOwnedCategory(ctx, cmd.ActorID, cmd.ParentRef)
		if err != nil {
			return CategoryResult{}, attachField(err, "parent_ref")
		}
		id := parent.ID()
		parentIDPtr = &id
	}

	category, err := ledger.NewCategory(s.IDs.NewID(), cmd.ActorID, parentIDPtr, name, kind, cmd.SortOrder, nil)
	if err != nil {
		return CategoryResult{}, wrapLedgerError(err)
	}

	if err := s.Categories.Create(ctx, cmd.ActorID, category); err != nil {
		return CategoryResult{}, err
	}
	return CategoryResult{Category: category}, nil
}

// RenameCategoryCommand renames an existing category, leaving its parent,
// kind, sort order, and archived state untouched.
type RenameCategoryCommand struct {
	ActorID     string
	CategoryRef string
	Name        string
}

// RenameCategory implements the "rename" use case in issue #6's
// categories scope.
func (s *Service) RenameCategory(ctx context.Context, cmd RenameCategoryCommand) (CategoryResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return CategoryResult{}, err
	}
	existing, err := s.resolveOwnedCategory(ctx, cmd.ActorID, cmd.CategoryRef)
	if err != nil {
		return CategoryResult{}, attachField(err, "category_ref")
	}

	name, err := normalize.Text(cmd.Name, maxCategoryNameLen)
	if err != nil {
		return CategoryResult{}, attachField(err, "name")
	}
	if name == "" {
		return CategoryResult{}, errs.New(errs.InvalidInput).Explain("A name is required.").Field("name")
	}

	updated, err := ledger.NewCategory(
		existing.ID(), existing.UserID(), categoryParentPtr(existing), name, existing.Kind(),
		existing.SortOrder(), categoryArchivedAtPtr(existing),
	)
	if err != nil {
		return CategoryResult{}, wrapLedgerError(err)
	}

	if err := s.Categories.Update(ctx, cmd.ActorID, updated); err != nil {
		return CategoryResult{}, err
	}
	return CategoryResult{Category: updated}, nil
}

// ReparentCategoryCommand moves a category to a new parent, or to
// top-level when ParentRef is empty. ParentRef must resolve against the
// actor's own categories, the same authorisation-by-candidate-list
// discipline every ref resolution in this package follows.
type ReparentCategoryCommand struct {
	ActorID     string
	CategoryRef string
	ParentRef   string
}

// ReparentCategory implements the "reparent" use case in issue #6's
// categories scope. The self-parent check (a category can't be its own
// parent) and the deeper-cycle check (a category can't be its own
// ancestor several levels up) both happen below this method, in
// ledger.NewCategory and the repository's checkNoCycle respectively — this
// method doesn't duplicate either.
func (s *Service) ReparentCategory(ctx context.Context, cmd ReparentCategoryCommand) (CategoryResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return CategoryResult{}, err
	}
	existing, err := s.resolveOwnedCategory(ctx, cmd.ActorID, cmd.CategoryRef)
	if err != nil {
		return CategoryResult{}, attachField(err, "category_ref")
	}

	var parentIDPtr *string
	if strings.TrimSpace(cmd.ParentRef) != "" {
		parent, err := s.resolveOwnedCategory(ctx, cmd.ActorID, cmd.ParentRef)
		if err != nil {
			return CategoryResult{}, attachField(err, "parent_ref")
		}
		id := parent.ID()
		parentIDPtr = &id
	}

	updated, err := ledger.NewCategory(
		existing.ID(), existing.UserID(), parentIDPtr, existing.Name(), existing.Kind(),
		existing.SortOrder(), categoryArchivedAtPtr(existing),
	)
	if err != nil {
		return CategoryResult{}, wrapLedgerError(err)
	}

	if err := s.Categories.Update(ctx, cmd.ActorID, updated); err != nil {
		return CategoryResult{}, err
	}
	return CategoryResult{Category: updated}, nil
}

// ArchiveCategoryCommand hides a category from pickers while leaving
// transactions that already reference it valid (data-model.md §6).
// Archiving an already-archived category is a no-op, for the same reason
// ArchiveAccount is idempotent (see its doc comment).
type ArchiveCategoryCommand struct {
	ActorID     string
	CategoryRef string
}

// ArchiveCategory implements the "archive" use case in issue #6's
// categories scope.
func (s *Service) ArchiveCategory(ctx context.Context, cmd ArchiveCategoryCommand) (CategoryResult, error) {
	if err := requireActorID(cmd.ActorID); err != nil {
		return CategoryResult{}, err
	}
	existing, err := s.resolveOwnedCategory(ctx, cmd.ActorID, cmd.CategoryRef)
	if err != nil {
		return CategoryResult{}, attachField(err, "category_ref")
	}
	if existing.Archived() {
		return CategoryResult{Category: existing}, nil
	}

	today, err := normalize.DateOf("", s.Clock, s.Config.UserTimezone)
	if err != nil {
		return CategoryResult{}, err
	}

	updated, err := ledger.NewCategory(
		existing.ID(), existing.UserID(), categoryParentPtr(existing), existing.Name(), existing.Kind(),
		existing.SortOrder(), &today,
	)
	if err != nil {
		return CategoryResult{}, wrapLedgerError(err)
	}

	if err := s.Categories.Update(ctx, cmd.ActorID, updated); err != nil {
		return CategoryResult{}, err
	}
	return CategoryResult{Category: updated}, nil
}

// ListCategoriesQuery lists every category actorID owns, flat (not as a
// tree) - see ListCategoryTree for the tree-shaped equivalent issue #6
// asks for.
type ListCategoriesQuery struct {
	ActorID string
}

// ListCategoriesResult is ListCategories' result, in the repository's name
// order.
type ListCategoriesResult struct {
	Categories []ledger.Category
}

// ListCategories lists every category actorID owns, flat. Most callers
// want ListCategoryTree instead; this exists because a flat list is
// sometimes exactly what's needed (populating a picker, resolving a ref)
// and building a tree only to flatten it again would be wasted work.
func (s *Service) ListCategories(ctx context.Context, q ListCategoriesQuery) (ListCategoriesResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ListCategoriesResult{}, err
	}
	categories, err := s.Categories.List(ctx, q.ActorID)
	if err != nil {
		return ListCategoriesResult{}, err
	}
	return ListCategoriesResult{Categories: categories}, nil
}

// CategoryNode is one node of the tree ListCategoryTree returns: a
// Category alongside its direct children, recursively.
type CategoryNode struct {
	Category ledger.Category
	Children []CategoryNode
}

// ListCategoryTreeQuery lists every category actorID owns, as a tree.
type ListCategoryTreeQuery struct {
	ActorID string
}

// ListCategoryTreeResult is ListCategoryTree's result: every top-level
// category, each with its descendants nested underneath.
type ListCategoryTreeResult struct {
	Roots []CategoryNode
}

// ListCategoryTree implements the "list as a tree" use case in issue #6's
// categories scope. The tree is built in Go from a single flat List call —
// there's no separate tree-shaped storage or query (data-model.md §6:
// "one hierarchy... reports roll up the full subtree"), and depth isn't
// constrained by the schema, so this recurses to whatever depth the data
// actually has rather than assuming the two levels the UI and seed data
// expect.
func (s *Service) ListCategoryTree(ctx context.Context, q ListCategoryTreeQuery) (ListCategoryTreeResult, error) {
	if err := requireActorID(q.ActorID); err != nil {
		return ListCategoryTreeResult{}, err
	}
	categories, err := s.Categories.List(ctx, q.ActorID)
	if err != nil {
		return ListCategoryTreeResult{}, err
	}
	return ListCategoryTreeResult{Roots: buildCategoryTree(categories)}, nil
}

// buildCategoryTree groups categories by parent ID (the empty string
// standing in for "top-level", matching Category.ParentID()'s (string,
// bool) convention collapsed to a map key) and recursively assembles
// CategoryNodes starting from the top-level group. It trusts that the
// category graph is already acyclic — the only write paths that can
// produce a Category (CreateCategory, ReparentCategory, and the
// repository's own checkNoCycle) all refuse to introduce a cycle, so
// there's nothing here guarding against unbounded recursion.
func buildCategoryTree(categories []ledger.Category) []CategoryNode {
	childrenByParent := make(map[string][]ledger.Category, len(categories))
	for _, c := range categories {
		parentID, _ := c.ParentID()
		childrenByParent[parentID] = append(childrenByParent[parentID], c)
	}

	var build func(parentID string) []CategoryNode
	build = func(parentID string) []CategoryNode {
		kids := childrenByParent[parentID]
		if len(kids) == 0 {
			return nil
		}
		nodes := make([]CategoryNode, 0, len(kids))
		for _, c := range kids {
			nodes = append(nodes, CategoryNode{Category: c, Children: build(c.ID())})
		}
		return nodes
	}
	return build("")
}
