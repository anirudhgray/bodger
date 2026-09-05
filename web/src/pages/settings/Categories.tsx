// The Categories subpage of Settings (issue #107, split out of the old
// Settings.tsx). No behavior change from the original CategoriesSection —
// only its own route now, rendered inside SettingsLayout's <Outlet />.
// The category hierarchy display itself (tree/indentation) is #106's
// scope, not this issue's — this flat list is deliberately left as-is,
// just relocated.
import { useEffect, useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { type Category, type CategoryKind } from '@/lib/api'
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

export function CategoriesSettings() {
  const [categories, setCategories] = useState<Category[] | null>(null)
  const [name, setName] = useState('')
  const [type, setType] = useState(CATEGORY_TYPES[0])
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [renamingID, setRenamingID] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')

  async function refresh() {
    try {
      setCategories(await listCategories())
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  useEffect(() => {
    refresh()
  }, [])

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setCreating(true)
    try {
      await createCategory({ name, type })
      setName('')
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setCreating(false)
    }
  }

  async function handleRename(id: string) {
    setError(null)
    try {
      await renameCategory(id, renameValue)
      setRenamingID(null)
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  async function handleArchive(id: string) {
    setError(null)
    // See TransactionsList.tsx's handleDelete for why this has a loading
    // toast but no `error` option, and why the action is awaited
    // separately from the toast.promise call.
    const archiving = (async () => {
      await archiveCategory(id)
      await refresh()
    })()
    toast.promise(archiving, {
      loading: 'Archiving…',
      success: 'Category archived.',
    })
    try {
      await archiving
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  async function handleReparent(id: string, parent: string) {
    setError(null)
    try {
      await reparentCategory(id, parent)
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
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
            id="category-type"
            value={type}
            onChange={(event) => setType(event.target.value as CategoryKind)}
          >
            {CATEGORY_TYPES.map((t) => (
              <option key={t} value={t}>
                {categoryKindLabel(t)}
              </option>
            ))}
          </Select>
        </div>
        <Button type="submit" disabled={creating || name === ''}>
          {creating ? 'Adding…' : 'Add'}
        </Button>
      </form>

      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
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
            {categories
              .filter((c) => !c.archived)
              .map((category) => (
                <li
                  key={category.id}
                  className="flex flex-col gap-2 px-4 py-2.5 text-sm sm:flex-row sm:items-center sm:justify-between"
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
                        <Select
                          id={`parent-${category.id}`}
                          className="h-7 text-xs"
                          value={category.parent_id ?? ''}
                          onChange={(event) =>
                            handleReparent(category.id, event.target.value)
                          }
                        >
                          <option value="">Top level</option>
                          {(categories ?? [])
                            // Same kind only — an expense category can't
                            // sit under an income parent or vice versa
                            // (one typed tree, data-model.md §6). The app
                            // layer (categories.go's
                            // categoryKindMismatchError) enforces this
                            // too; filtering here just keeps the picker
                            // from offering a choice it would reject.
                            .filter(
                              (c) =>
                                c.id !== category.id &&
                                c.type === category.type,
                            )
                            .map((c) => (
                              <option key={c.id} value={c.id}>
                                {c.name}
                              </option>
                            ))}
                        </Select>
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
              ))}
          </ul>
        </Card>
      )}
    </section>
  )
}
