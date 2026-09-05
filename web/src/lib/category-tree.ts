// Turns the flat Category[] every list/create endpoint returns
// (internal/domain/ledger/category.go's ParentID, the REST API's
// parent_id field) into an ordered, depth-annotated walk of the tree it
// actually describes — issue #106's one tree-building function, reused
// by every place that needs to *show* the hierarchy rather than three
// separate copies of the same parent_id-chasing logic: Settings'
// category list (pages/settings/Categories.tsx), and every category
// picker (components/ui/combobox.tsx call sites in TransactionDialog,
// TransactionsList, and Categories.tsx's own "Parent" field).
//
// Lives in lib/, not pages/settings/shared.ts, because it isn't
// settings-specific — TransactionDialog and TransactionsList need the
// exact same tree outside of Settings entirely.
import type { Category } from '@/lib/api'

export interface CategoryTreeNode {
  category: Category
  // 0 for a top-level category, 1 for its direct children, and so on —
  // categories can nest arbitrarily deep (no cap in the domain model),
  // so this is a plain count of parent hops, not a fixed enum.
  depth: number
  // The full "Parent > Child > Grandchild" chain down to this category,
  // for display contexts (a combobox's collapsed trigger, a "Parent"
  // field's own selected value) where indentation alone would be
  // invisible once the tree is no longer laid out as a list.
  path: string
}

// buildCategoryTree returns every category in `categories` exactly once,
// each annotated with its depth and full ancestry path, ordered as a
// depth-first walk (a parent immediately followed by all its
// descendants) — the order a flat indented list should render in.
// Siblings are ordered by sort_order, then name, matching how the CLI's
// own `categories tree` output orders things.
export function buildCategoryTree(categories: Category[]): CategoryTreeNode[] {
  const byParentID = new Map<string, Category[]>()
  for (const category of categories) {
    const key = category.parent_id ?? ''
    const siblings = byParentID.get(key)
    if (siblings) siblings.push(category)
    else byParentID.set(key, [category])
  }
  for (const siblings of byParentID.values()) {
    siblings.sort(
      (a, b) => a.sort_order - b.sort_order || a.name.localeCompare(b.name),
    )
  }

  const result: CategoryTreeNode[] = []
  // Every category is added at most once, however its parent_id chain
  // is shaped — this is what keeps a cycle (which the domain
  // constructor and reparent path both already reject, but a defensive
  // client-side guard is cheap) from recursing forever instead of just
  // producing a slightly wrong tree.
  const seen = new Set<string>()

  function walk(parentKey: string, depth: number, parentPath: string) {
    for (const category of byParentID.get(parentKey) ?? []) {
      if (seen.has(category.id)) continue
      seen.add(category.id)
      const path = parentPath
        ? `${parentPath} > ${category.name}`
        : category.name
      result.push({ category, depth, path })
      walk(category.id, depth + 1, path)
    }
  }

  walk('', 0, '')

  // A category whose parent_id points outside `categories` — most
  // likely an archived parent filtered out of the array while this
  // child is still active, or still selected — has nowhere to attach
  // above. Surface it as top-level rather than silently dropping it
  // from whatever picker is rendering this tree.
  for (const category of categories) {
    if (!seen.has(category.id)) {
      seen.add(category.id)
      result.push({ category, depth: 0, path: category.name })
    }
  }

  return result
}
