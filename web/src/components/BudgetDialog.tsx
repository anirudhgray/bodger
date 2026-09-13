// Create/edit dialog for a budget and its lines (issue #245). Follows
// TransactionDialog.tsx's own shape closely (a controlled Dialog, one
// useState per field, sanitizeAmountInput for money inputs, toast-only
// failure reporting per docs/design-system.md's "Toasts vs. inline
// messages") — the two differ mainly in that a budget's lines are a
// variable-length, addable/removable list rather than a fixed field set.
//
// A line's category is immutable once created (#244's own constraint,
// internal/app.UpdateBudgetLineCommand's doc comment) — applyLineChanges
// below handles that by removing and re-adding a line whose category was
// changed in the form, rather than attempting an unsupported in-place
// category change.
import { useMemo, useState, type FormEvent } from 'react'
import { Plus, X } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Combobox, type ComboboxOption } from '@/components/ui/combobox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { DatePicker } from '@/components/ui/date-picker'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  addBudgetLine,
  createBudget,
  removeBudgetLine,
  updateBudget,
  updateBudgetLine,
  ApiError,
  type Budget,
  type BudgetLine,
  type Category,
} from '@/lib/api'
import { buildCategoryTree } from '@/lib/category-tree'
import { sanitizeAmountInput } from '@/lib/utils'

function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : 'Couldn’t reach the server. Try again.'
}

// LineForm is one row of the editable lines list. `key` is a stable React
// key independent of `id` — a newly added row has no server id yet — and
// `id` is only present for a line that already exists on the budget
// (present in `budget.lines` when the dialog opened), which is what
// applyLineChanges below uses to tell an edit from a brand-new line.
type LineForm = {
  key: string
  id?: string
  categoryId: string
  amount: string
  rollover: boolean
}

let lineKeySeq = 0
function newLineKey(): string {
  lineKeySeq += 1
  return `new-${lineKeySeq}`
}

function linesFromBudget(budget: Budget | null): LineForm[] {
  if (!budget) return []
  return budget.lines.map((l) => ({
    key: l.id,
    id: l.id,
    categoryId: l.category_id,
    amount: l.amount,
    rollover: l.rollover,
  }))
}

// applyLineChanges diffs the form's current lines against the budget's
// original lines and issues exactly the add/update/remove calls needed to
// bring the server in line — sequential, not Promise.all, so a partial
// failure leaves a predictable prefix applied rather than an unordered mix
// of whichever calls happened to finish first.
async function applyLineChanges(
  budgetId: string,
  original: BudgetLine[],
  form: LineForm[],
): Promise<void> {
  const keptIds = new Set(form.filter((l) => l.id).map((l) => l.id))
  for (const line of original) {
    if (!keptIds.has(line.id)) {
      await removeBudgetLine(budgetId, line.id)
    }
  }
  for (const line of form) {
    const existing = original.find((o) => o.id === line.id)
    if (!existing) {
      await addBudgetLine(budgetId, {
        categoryRef: line.categoryId,
        amount: line.amount,
        rollover: line.rollover,
      })
      continue
    }
    if (existing.category_id !== line.categoryId) {
      // Category can't change in place — remove the old line and add a
      // new one for the newly picked category instead.
      await removeBudgetLine(budgetId, existing.id)
      await addBudgetLine(budgetId, {
        categoryRef: line.categoryId,
        amount: line.amount,
        rollover: line.rollover,
      })
      continue
    }
    if (
      existing.amount !== line.amount ||
      existing.rollover !== line.rollover
    ) {
      await updateBudgetLine(budgetId, existing.id, {
        amount: line.amount,
        rollover: line.rollover,
      })
    }
  }
}

export function BudgetDialog({
  open,
  onOpenChange,
  budget,
  categories,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  // null = create a new budget; otherwise edit this one.
  budget: Budget | null
  categories: Category[]
  onSaved: () => void
}) {
  const isEdit = budget !== null

  const [name, setName] = useState(budget?.name ?? '')
  // Only read in create mode — a budget's currency can't change once
  // created (#244), so the edit form shows it as plain text instead.
  const [currency, setCurrency] = useState('')
  const [startsOn, setStartsOn] = useState(budget?.starts_on ?? '')
  const [lines, setLines] = useState<LineForm[]>(() => linesFromBudget(budget))
  const [submitting, setSubmitting] = useState(false)

  const categoryOptions = useMemo<ComboboxOption[]>(
    () =>
      buildCategoryTree(categories).map(({ category, depth, path }) => ({
        value: category.id,
        label: category.name,
        depth,
        path,
      })),
    [categories],
  )

  function addLine() {
    setLines((prev) => [
      ...prev,
      { key: newLineKey(), categoryId: '', amount: '', rollover: false },
    ])
  }

  function removeLine(key: string) {
    setLines((prev) => prev.filter((l) => l.key !== key))
  }

  function updateLine(key: string, patch: Partial<LineForm>) {
    setLines((prev) =>
      prev.map((l) => (l.key === key ? { ...l, ...patch } : l)),
    )
  }

  const canSubmit =
    name.trim() !== '' &&
    lines.every((l) => l.categoryId !== '' && l.amount.trim() !== '')

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canSubmit) return
    setSubmitting(true)
    try {
      if (isEdit) {
        // PATCH replaces both fields together — resend the budget's
        // existing starts_on when the field is unchanged, rather than
        // letting an empty value silently reset it to today.
        await updateBudget(budget.id, {
          name: name.trim(),
          startsOn: startsOn || budget.starts_on,
        })
        await applyLineChanges(budget.id, budget.lines, lines)
        toast.success('Budget saved.')
      } else {
        await createBudget({
          name: name.trim(),
          currency: currency.trim() || undefined,
          startsOn: startsOn || undefined,
          lines: lines.map((l) => ({
            categoryRef: l.categoryId,
            amount: l.amount,
            rollover: l.rollover,
          })),
        })
        toast.success('Budget created.')
      }
      onOpenChange(false)
      onSaved()
    } catch (err) {
      // Action failure — reports via toast only, per docs/design-system.md
      // ("Toasts vs. inline messages"); the dialog stays open with
      // everything typed still in place.
      toast.error(errorMessage(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{isEdit ? 'Edit budget' : 'New budget'}</DialogTitle>
          <DialogDescription>
            {isEdit
              ? 'Rename this budget, move its start date, or change its lines.'
              : 'Plan an amount per category for a monthly cycle.'}
          </DialogDescription>
        </DialogHeader>

        <form
          className="flex min-h-0 flex-col gap-4"
          onSubmit={handleSubmit}
          noValidate
        >
          <div className="no-scrollbar -mx-4 flex max-h-[70vh] flex-col gap-4 overflow-y-auto px-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="budget-name">Name</Label>
              <Input
                id="budget-name"
                required
                autoFocus
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>

            <div className="flex flex-wrap gap-4">
              {!isEdit && (
                <div className="flex flex-1 flex-col gap-1.5">
                  <Label htmlFor="budget-currency">Currency</Label>
                  <Input
                    id="budget-currency"
                    placeholder="Defaults to your reporting currency"
                    value={currency}
                    onChange={(e) => setCurrency(e.target.value.toUpperCase())}
                  />
                </div>
              )}
              {isEdit && (
                <div className="flex flex-1 flex-col gap-1.5">
                  <Label>Currency</Label>
                  <p className="text-muted-foreground text-sm">
                    {budget.currency} (can’t be changed)
                  </p>
                </div>
              )}
              <div className="flex flex-1 flex-col gap-1.5">
                <Label htmlFor="budget-starts-on">Starts on</Label>
                <DatePicker
                  id="budget-starts-on"
                  value={startsOn}
                  onChange={setStartsOn}
                />
              </div>
            </div>

            <div className="flex flex-col gap-2 border-t pt-4">
              <div className="flex items-center justify-between">
                <Label>Lines</Label>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={addLine}
                >
                  <Plus /> Add line
                </Button>
              </div>
              <p className="text-muted-foreground text-xs">
                Rollover carries a line’s unspent (or overspent) amount into the
                next period — recorded now, not yet acted on anywhere.
              </p>

              {lines.length === 0 && (
                <p className="text-muted-foreground text-sm">
                  No lines yet. Add one to plan an amount for a category.
                </p>
              )}

              {lines.map((line) => (
                <div
                  key={line.key}
                  className="flex flex-wrap items-center gap-2"
                >
                  <Combobox
                    id={`budget-line-category-${line.key}`}
                    aria-label="Category"
                    className="min-w-40 flex-1"
                    options={categoryOptions}
                    value={line.categoryId}
                    onChange={(value) =>
                      updateLine(line.key, { categoryId: value })
                    }
                    placeholder="Choose a category"
                    searchPlaceholder="Search categories…"
                    emptyText="No matching categories."
                  />
                  <Input
                    aria-label="Amount"
                    inputMode="decimal"
                    placeholder="0.00"
                    className="w-24"
                    value={line.amount}
                    onChange={(e) =>
                      updateLine(line.key, {
                        amount: sanitizeAmountInput(e.target.value),
                      })
                    }
                  />
                  <Label className="flex items-center gap-1.5 text-xs whitespace-nowrap">
                    <Switch
                      checked={line.rollover}
                      onCheckedChange={(checked) =>
                        updateLine(line.key, { rollover: checked })
                      }
                    />
                    Rollover
                  </Label>
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    onClick={() => removeLine(line.key)}
                  >
                    <X />
                    <span className="sr-only">Remove line</span>
                  </Button>
                </div>
              ))}
            </div>
          </div>

          <DialogFooter>
            <Button type="submit" disabled={!canSubmit || submitting}>
              {submitting ? 'Saving…' : isEdit ? 'Save' : 'Create budget'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
