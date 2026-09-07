// The Categories subpage of Settings (issue #107, split out of the old
// Settings.tsx). Its list and "Parent" field now show the real
// parent_id hierarchy (issue #106) via lib/category-tree.ts's
// buildCategoryTree — the same helper TransactionDialog's and
// TransactionsList's category pickers use, so all three agree on what
// the tree looks like. Everything else (create/rename/archive/reparent
// themselves) is unchanged from the original CategoriesSection.
import { useEffect, useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Combobox, type ComboboxOption } from '@/components/ui/combobox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { type Category, type CategoryKind } from '@/lib/api'
import { buildCategoryTree } from '@/lib/category-tree'
import {
  archiveCategory,
  createCategory,
  listCategories,
  renameCategory,
  reparentCategory,
} from '@/lib/settings'
import { errorMessage } from './shared'

// categoryKindLabel: see Accounts.tsx's accountKindLabel — same
// rationale, for CategoryKind's expense/income wire values.
function categoryKindLabel(kind: CategoryKind): string {
  switch (kind) {
    case 'expense':
      return 'Expense'
    case 'income':
      return 'Income'
  }
}

const CATEGORY_TYPES: CategoryKind[] = ['expense', 'income']

// The "Parent" combobox's own option list for one category row: every
// other category of the same kind (an expense category can't sit under
// an income parent or vice versa — one typed tree, data-model.md §6;
// the app layer enforces this too, filtering here just keeps the picker
// from offering a choice it would reject), shown with the same
// indentation/path hierarchy as everywhere else. A category can't be
// its own parent, so it's excluded from its own candidate list — this
// is display-only and doesn't attempt to also exclude the category's
// *descendants* (reparenting under one would form a cycle the server
// already rejects), matching this field's pre-#106 behavior exactly.
function parentOptionsFor(
  category: Category,
  categories: Category[],
): ComboboxOption[] {
  const candidates = categories.filter(
    (c) => c.id !== category.id && c.type === category.type,
  )
  return [
    { value: '', label: 'Top level' },
    ...buildCategoryTree(candidates).map(({ category: c, depth, path }) => ({
      value: c.id,
      label: c.name,
      depth,
      path,
    })),
  ]
}

export function CategoriesSettings() {
  const [categories, setCategories] = useState<Category[] | null>(null)
  const [name, setName] = useState('')
  const [type, setType] = useState(CATEGORY_TYPES[0])
  const [creating, setCreating] = useState(false)
  // The listCategories() load failure only — create/rename/archive/
  // reparent are all user-triggered actions, and report their own
  // failure via toast instead (docs/design-system.md's "Toasts vs.
  // inline messages").
  const [loadError, setLoadError] = useState<string | null>(null)
  const [renamingID, setRenamingID] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')

  async function refresh() {
    try {
      setCategories(await listCategories())
    } catch (err) {
      setLoadError(errorMessage(err))
    }
  }

  useEffect(() => {
    refresh()
  }, [])

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreating(true)
    try {
      await createCategory({ name, type })
      setName('')
      await refresh()
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setCreating(false)
    }
  }

  async function handleRename(id: string) {
    try {
      await renameCategory(id, renameValue)
      setRenamingID(null)
      await refresh()
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  async function handleArchive(id: string) {
    // See TransactionsList.tsx's handleDelete for why the action is
    // awaited separately from the toast.promise call.
    const archiving = (async () => {
      await archiveCategory(id)
      await refresh()
    })()
    toast.promise(archiving, {
      loading: 'Archiving…',
      success: 'Category archived.',
      error: (err) => errorMessage(err),
    })
    try {
      await archiving
    } catch {
      // Failure is already reported via the toast.promise `error` option
      // above.
    }
  }

  async function handleReparent(id: string, parent: string) {
    try {
      await reparentCategory(id, parent)
      await refresh()
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <section className="flex flex-col gap-4">
      <p className="text-muted-foreground text-sm">
        How your income and expenses are grouped.
      </p>

      <form
        onSubmit={handleCreate}
        className="flex max-w-md flex-wrap items-end gap-2"
      >
        <div className="flex flex-1 flex-col gap-1.5">
          <Label htmlFor="category-name">New category name</Label>
          <Input
            id="category-name"
            required
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="category-type">Type</Label>
          <Select
            value={type}
            onValueChange={(value) => setType(value as CategoryKind)}
          >
            <SelectTrigger id="category-type">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {CATEGORY_TYPES.map((t) => (
                <SelectItem key={t} value={t}>
                  {categoryKindLabel(t)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button type="submit" disabled={creating || name === ''}>
          {creating ? 'Adding…' : 'Add'}
        </Button>
      </form>

      {loadError && (
        <p role="alert" className="text-destructive text-sm">
          {loadError}
        </p>
      )}

      {categories === null ? (
        <div className="flex items-center gap-2">
          <Spinner />
          <span className="text-muted-foreground text-sm">Loading…</span>
        </div>
      ) : (
        <Card className="[--card-spacing:0]">
          <ul className="divide-border divide-y">
            {buildCategoryTree(categories.filter((c) => !c.archived)).map(
              ({ category, depth }) => (
                <li
                  key={category.id}
                  className="flex flex-col gap-2 px-4 py-2.5 text-sm sm:flex-row sm:items-center sm:justify-between"
                  // Indentation reflecting this category's parent_id
                  // chain (issue #106) — added on top of the row's own
                  // px-4 base inset, not in place of it, so a top-level
                  // category (depth 0) sits exactly where it always did.
                  style={{ paddingLeft: `${16 + depth * 20}px` }}
                >
                  {renamingID === category.id ? (
                    <form
                      className="flex flex-1 flex-wrap items-center gap-2"
                      onSubmit={(event) => {
                        event.preventDefault()
                        handleRename(category.id)
                      }}
                    >
                      <Input
                        autoFocus
                        value={renameValue}
                        onChange={(event) => setRenameValue(event.target.value)}
                      />
                      <Button type="submit" size="sm">
                        Save
                      </Button>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => setRenamingID(null)}
                      >
                        Cancel
                      </Button>
                    </form>
                  ) : (
                    <>
                      <div>
                        <p className="font-medium">{category.name}</p>
                        <p className="text-muted-foreground text-xs">
                          {categoryKindLabel(category.type)}
                        </p>
                      </div>
                      <div className="flex flex-wrap items-center gap-2">
                        <Label
                          htmlFor={`parent-${category.id}`}
                          className="text-muted-foreground text-xs font-normal"
                        >
                          Parent
                        </Label>
                        <Combobox
                          id={`parent-${category.id}`}
                          className="h-7 w-44 text-xs"
                          value={category.parent_id ?? ''}
                          onChange={(parent) =>
                            handleReparent(category.id, parent)
                          }
                          options={parentOptionsFor(category, categories ?? [])}
                          placeholder="Top level"
                          searchPlaceholder="Search categories…"
                          emptyText="No matching categories."
                        />
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => {
                            setRenamingID(category.id)
                            setRenameValue(category.name)
                          }}
                        >
                          Rename
                        </Button>
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => handleArchive(category.id)}
                        >
                          Archive
                        </Button>
                      </div>
                    </>
                  )}
                </li>
              ),
            )}
          </ul>
        </Card>
      )}
    </section>
  )
}
