package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/anirudhgray/bodger/internal/app"
	"github.com/anirudhgray/bodger/internal/ports"
)

// categoryView is categories' counterpart to accounts.go's accountView —
// see its doc comment for why every field here is a plain primitive.
type categoryView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ParentID   string `json:"parent_id,omitempty"`
	SortOrder  int    `json:"sort_order"`
	Archived   bool   `json:"archived"`
	ArchivedAt string `json:"archived_at,omitempty"`
}

func categoryViewFrom(r app.CategoryResult) categoryView {
	v := categoryView{
		ID:        r.Category.ID(),
		Name:      r.Category.Name(),
		Type:      string(r.Category.Kind()),
		SortOrder: r.Category.SortOrder(),
		Archived:  r.Category.Archived(),
	}
	if pid, ok := r.Category.ParentID(); ok {
		v.ParentID = pid
	}
	if d, ok := r.Category.ArchivedAt(); ok {
		v.ArchivedAt = d.String()
	}
	return v
}

func printCategory(w io.Writer, v categoryView) {
	_, _ = fmt.Fprintf(w, "%s (%s)\n", v.Name, v.Type)
	_, _ = fmt.Fprintf(w, "id: %s\n", v.ID)
}

func printCategoryTable(w io.Writer, views []categoryView) {
	if len(views) == 0 {
		_, _ = fmt.Fprintln(w, "No categories yet. Add one with `bodger categories add`.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tTYPE\tPARENT\tARCHIVED")
	for _, v := range views {
		archived := "no"
		if v.Archived {
			archived = "yes"
		}
		parent := v.ParentID
		if parent == "" {
			parent = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", v.Name, v.Type, parent, archived)
	}
	_ = tw.Flush()
}

// newCategoriesCmd builds the "categories" command group: list, add,
// rename, and archive, matching issue #7's scope exactly. ReparentCategory
// and ListCategoryTree exist at the application layer
// (internal/app/categories.go) but aren't wired to commands here, for the
// same reason accounts.go's newAccountsCmd doesn't expose RenameAccount —
// the issue's worked command list doesn't call for them.
func newCategoriesCmd(factory ServiceFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "categories",
		Short: "Manage your categories",
	}
	cmd.AddCommand(
		newCategoriesListCmd(factory),
		newCategoriesAddCmd(factory),
		newCategoriesRenameCmd(factory),
		newCategoriesArchiveCmd(factory),
	)
	return cmd
}

func newCategoriesListCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your categories",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ListCategories(ctx, app.ListCategoriesQuery{ActorID: ports.SeededUserID})
			if err != nil {
				return err
			}
			views := make([]categoryView, len(result.Categories))
			for i, c := range result.Categories {
				views[i] = categoryViewFrom(app.CategoryResult{Category: c})
			}
			return render(cmd, views, func(w io.Writer) { printCategoryTable(w, views) })
		},
	}
}

type categoryAddFlags struct {
	categoryType string
	parent       string
	sortOrder    int
}

func newCategoriesAddCmd(factory ServiceFactory) *cobra.Command {
	var f categoryAddFlags
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a new category",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.CreateCategory(ctx, app.CreateCategoryCommand{
				ActorID:   ports.SeededUserID,
				Name:      args[0],
				Kind:      f.categoryType,
				ParentRef: f.parent,
				SortOrder: f.sortOrder,
			})
			if err != nil {
				return err
			}
			view := categoryViewFrom(result)
			return render(cmd, view, func(w io.Writer) { printCategory(w, view) })
		},
	}
	cmd.Flags().StringVar(&f.categoryType, "type", "", "category type: expense or income (required)")
	cmd.Flags().StringVar(&f.parent, "parent", "", "the parent category, if this belongs under one (defaults to top-level)")
	cmd.Flags().IntVar(&f.sortOrder, "sort-order", 0, "where this category sorts in lists")
	_ = cmd.MarkFlagRequired("type")
	return cmd
}

func newCategoriesRenameCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <category> <new-name>",
		Short: "Rename a category",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.RenameCategory(ctx, app.RenameCategoryCommand{
				ActorID:     ports.SeededUserID,
				CategoryRef: args[0],
				Name:        args[1],
			})
			if err != nil {
				return err
			}
			view := categoryViewFrom(result)
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Renamed category to %q.\n", view.Name)
			})
		},
	}
}

func newCategoriesArchiveCmd(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "archive <category>",
		Short: "Archive a category, hiding it from pickers while keeping past transactions valid",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, closeDB, err := factory(ctx)
			if err != nil {
				return err
			}
			defer closeQuietly(cmd, closeDB)

			result, err := svc.ArchiveCategory(ctx, app.ArchiveCategoryCommand{
				ActorID:     ports.SeededUserID,
				CategoryRef: args[0],
			})
			if err != nil {
				return err
			}
			view := categoryViewFrom(result)
			return render(cmd, view, func(w io.Writer) {
				_, _ = fmt.Fprintf(w, "Archived category %q.\n", view.Name)
			})
		},
	}
}
