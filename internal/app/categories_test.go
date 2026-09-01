package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/platform/errs"
)

func TestCreateCategory_TopLevelAndChild(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	food, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Food", Kind: "expense"})
	if err != nil {
		t.Fatalf("CreateCategory(Food): %v", err)
	}
	if _, ok := food.Category.ParentID(); ok {
		t.Error("top-level category should have no parent")
	}

	groceries, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{
		ActorID: testActorID, Name: "Groceries", Kind: "expense", ParentRef: "Food",
	})
	if err != nil {
		t.Fatalf("CreateCategory(Groceries): %v", err)
	}
	parentID, ok := groceries.Category.ParentID()
	if !ok || parentID != food.Category.ID() {
		t.Errorf("Groceries.ParentID() = %q, %v, want %q, true", parentID, ok, food.Category.ID())
	}
}

func TestCreateCategory_InvalidKind(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.CreateCategory(context.Background(), app.CreateCategoryCommand{ActorID: testActorID, Name: "Food", Kind: "not-a-kind"})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestCreateCategory_UnknownParentRef(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	_, err := svc.CreateCategory(context.Background(), app.CreateCategoryCommand{
		ActorID: testActorID, Name: "Groceries", Kind: "expense", ParentRef: "does-not-exist",
	})
	wantErrCode(t, err, errs.NotFound)
}

func TestRenameCategory(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	food, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Food", Kind: "expense"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	renamed, err := svc.RenameCategory(ctx, app.RenameCategoryCommand{ActorID: testActorID, CategoryRef: food.Category.ID(), Name: "Dining"})
	if err != nil {
		t.Fatalf("RenameCategory: %v", err)
	}
	if renamed.Category.Name() != "Dining" {
		t.Errorf("Name = %q, want %q", renamed.Category.Name(), "Dining")
	}
	if renamed.Category.Kind() != food.Category.Kind() {
		t.Error("RenameCategory must not change Kind")
	}
}

func TestReparentCategory(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	food, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Food", Kind: "expense"})
	if err != nil {
		t.Fatalf("CreateCategory(Food): %v", err)
	}
	misc, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Misc", Kind: "expense"})
	if err != nil {
		t.Fatalf("CreateCategory(Misc): %v", err)
	}
	snacks, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Snacks", Kind: "expense", ParentRef: food.Category.ID()})
	if err != nil {
		t.Fatalf("CreateCategory(Snacks): %v", err)
	}

	moved, err := svc.ReparentCategory(ctx, app.ReparentCategoryCommand{ActorID: testActorID, CategoryRef: snacks.Category.ID(), ParentRef: misc.Category.ID()})
	if err != nil {
		t.Fatalf("ReparentCategory: %v", err)
	}
	parentID, ok := moved.Category.ParentID()
	if !ok || parentID != misc.Category.ID() {
		t.Errorf("ParentID() = %q, %v, want %q, true", parentID, ok, misc.Category.ID())
	}

	// Reparenting to "" makes it top-level again.
	topLevel, err := svc.ReparentCategory(ctx, app.ReparentCategoryCommand{ActorID: testActorID, CategoryRef: snacks.Category.ID(), ParentRef: ""})
	if err != nil {
		t.Fatalf("ReparentCategory (to top-level): %v", err)
	}
	if _, ok := topLevel.Category.ParentID(); ok {
		t.Error("ReparentCategory with an empty ParentRef should make the category top-level")
	}
}

func TestReparentCategory_RejectsSelfParent(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	food, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Food", Kind: "expense"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	_, err = svc.ReparentCategory(ctx, app.ReparentCategoryCommand{ActorID: testActorID, CategoryRef: food.Category.ID(), ParentRef: food.Category.ID()})
	wantErrCode(t, err, errs.InvalidInput)
}

func TestArchiveCategory(t *testing.T) {
	frozen := time.Date(2026, time.August, 14, 10, 0, 0, 0, time.UTC)
	svc := newTestService(t, frozen, "UTC")
	ctx := context.Background()

	food, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Food", Kind: "expense"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	archived, err := svc.ArchiveCategory(ctx, app.ArchiveCategoryCommand{ActorID: testActorID, CategoryRef: food.Category.ID()})
	if err != nil {
		t.Fatalf("ArchiveCategory: %v", err)
	}
	if !archived.Category.Archived() {
		t.Fatal("category should be archived")
	}

	// Idempotent re-archive.
	again, err := svc.ArchiveCategory(ctx, app.ArchiveCategoryCommand{ActorID: testActorID, CategoryRef: food.Category.ID()})
	if err != nil {
		t.Fatalf("ArchiveCategory (again): %v", err)
	}
	d1, _ := archived.Category.ArchivedAt()
	d2, _ := again.Category.ArchivedAt()
	if !d1.Equal(d2) {
		t.Errorf("re-archiving changed ArchivedAt from %v to %v", d1, d2)
	}
}

func TestListCategoryTree(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	food, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Food", Kind: "expense"})
	if err != nil {
		t.Fatalf("CreateCategory(Food): %v", err)
	}
	if _, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Groceries", Kind: "expense", ParentRef: food.Category.ID()}); err != nil {
		t.Fatalf("CreateCategory(Groceries): %v", err)
	}
	if _, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Restaurants", Kind: "expense", ParentRef: food.Category.ID()}); err != nil {
		t.Fatalf("CreateCategory(Restaurants): %v", err)
	}
	if _, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Salary", Kind: "income"}); err != nil {
		t.Fatalf("CreateCategory(Salary): %v", err)
	}

	result, err := svc.ListCategoryTree(ctx, app.ListCategoryTreeQuery{ActorID: testActorID})
	if err != nil {
		t.Fatalf("ListCategoryTree: %v", err)
	}
	if len(result.Roots) != 2 {
		t.Fatalf("len(Roots) = %d, want 2 (Food, Salary)", len(result.Roots))
	}

	var foodNode *app.CategoryNode
	for i := range result.Roots {
		if result.Roots[i].Category.Name() == "Food" {
			foodNode = &result.Roots[i]
		}
	}
	if foodNode == nil {
		t.Fatal("Food not found among Roots")
	}
	if len(foodNode.Children) != 2 {
		t.Errorf("len(Food.Children) = %d, want 2", len(foodNode.Children))
	}
}

func TestCategoryUseCases_CrossActorRefIsInvisible(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	food, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Food", Kind: "expense"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	_, err = svc.RenameCategory(ctx, app.RenameCategoryCommand{ActorID: "someone-else", CategoryRef: food.Category.ID(), Name: "Stolen"})
	wantErrCode(t, err, errs.NotFound)
}

func TestGetCategory(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	created, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{ActorID: testActorID, Name: "Food", Kind: "expense"})
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	byID, err := svc.GetCategory(ctx, app.GetCategoryQuery{ActorID: testActorID, CategoryRef: created.Category.ID()})
	if err != nil {
		t.Fatalf("GetCategory by ID: %v", err)
	}
	if byID.Category.ID() != created.Category.ID() {
		t.Errorf("GetCategory by ID = %+v, want %+v", byID.Category, created.Category)
	}
}

func TestGetCategory_UnknownRef(t *testing.T) {
	svc := newTestService(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), "UTC")
	ctx := context.Background()

	_, err := svc.GetCategory(ctx, app.GetCategoryQuery{ActorID: testActorID, CategoryRef: "nonexistent"})
	wantErrCode(t, err, errs.NotFound)
}
