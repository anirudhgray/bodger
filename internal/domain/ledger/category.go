package ledger

import "fmt"

// CategoryKind discriminates the one category hierarchy by which picker it
// belongs in — data-model.md §6: one tree, typed, rather than two trees or
// an untyped shared one (which would let "Salary" appear in an expense
// picker).
type CategoryKind string

const (
	CategoryKindExpense CategoryKind = "expense"
	CategoryKindIncome  CategoryKind = "income"
)

var validCategoryKinds = map[CategoryKind]bool{
	CategoryKindExpense: true,
	CategoryKindIncome:  true,
}

// Category is one node in the user's self-referential category tree. Its
// fields are unexported: the only way to produce one is NewCategory.
type Category struct {
	id       string
	userID   string
	parentID *string
	name     string
	kind     CategoryKind
	archived bool
}

// NewCategory constructs a Category. parentID may be nil for a top-level
// category; if set, it must not equal id — a category cannot be its own
// parent. Deeper cycles (A's parent is B, B's parent is A) need the full
// tree to detect and are a repository/application-layer concern, not this
// constructor's.
func NewCategory(id, userID string, parentID *string, name string, kind CategoryKind, archived bool) (Category, error) {
	if id == "" {
		return Category{}, ErrCategoryEmptyID
	}
	if userID == "" {
		return Category{}, ErrCategoryEmptyUserID
	}
	if name == "" {
		return Category{}, ErrCategoryEmptyName
	}
	if !validCategoryKinds[kind] {
		return Category{}, fmt.Errorf("%w: %q", ErrCategoryInvalidKind, kind)
	}
	if parentID != nil && *parentID == id {
		return Category{}, ErrCategorySelfParent
	}

	var pid *string
	if parentID != nil {
		p := *parentID
		pid = &p
	}

	return Category{
		id:       id,
		userID:   userID,
		parentID: pid,
		name:     name,
		kind:     kind,
		archived: archived,
	}, nil
}

// ID returns the category's identifier.
func (c Category) ID() string { return c.id }

// UserID returns the ID of the user who owns the category.
func (c Category) UserID() string { return c.userID }

// ParentID returns the parent category's ID, and false if this category
// is top-level.
func (c Category) ParentID() (string, bool) {
	if c.parentID == nil {
		return "", false
	}
	return *c.parentID, true
}

// Name returns the category's name.
func (c Category) Name() string { return c.name }

// Kind returns the category's kind.
func (c Category) Kind() CategoryKind { return c.kind }

// Archived reports whether the category is hidden from pickers.
func (c Category) Archived() bool { return c.archived }
