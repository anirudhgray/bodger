// Create/edit dialog for a recurring rule (issue #282), following
// BudgetDialog.tsx's own shape closely: a controlled Dialog, one useState
// per field, sanitizeAmountInput for the money input, toast-only failure
// reporting per docs/design-system.md's "Toasts vs. inline messages".
//
// account_ref/category_ref/starts_on are only ever sent on create —
// PATCH /api/v1/recurring-rules/{id}'s own contract doesn't accept any of
// the three (a rule posts to exactly one fixed account/category pair,
// data-model.md §11, and its start date doesn't move once created), so
// edit mode shows all three as plain read-only text instead of editable
// fields, the same treatment BudgetDialog gives a budget's currency.
import { useMemo, useState, type FormEvent } from 'react'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  ApiError,
  createRecurringRule,
  updateRecurringRule,
  type Account,
  type Category,
  type RecurringRule,
  type ScheduleInput,
} from '@/lib/api'
import { buildCategoryTree } from '@/lib/category-tree'
import { sanitizeAmountInput } from '@/lib/utils'

function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : 'Couldn’t reach the server. Try again.'
}

const WEEKDAY_OPTIONS = [
  { value: 0, label: 'Sunday' },
  { value: 1, label: 'Monday' },
  { value: 2, label: 'Tuesday' },
  { value: 3, label: 'Wednesday' },
  { value: 4, label: 'Thursday' },
  { value: 5, label: 'Friday' },
  { value: 6, label: 'Saturday' },
]

const MONTH_OPTIONS = [
  { value: 1, label: 'January' },
  { value: 2, label: 'February' },
  { value: 3, label: 'March' },
  { value: 4, label: 'April' },
  { value: 5, label: 'May' },
  { value: 6, label: 'June' },
  { value: 7, label: 'July' },
  { value: 8, label: 'August' },
  { value: 9, label: 'September' },
  { value: 10, label: 'October' },
  { value: 11, label: 'November' },
  { value: 12, label: 'December' },
]

function scheduleFromRule(rule: RecurringRule | null): ScheduleInput {
  if (!rule) return { frequency: 'monthly', interval: 1, dayOfMonth: 1 }
  return {
    frequency: rule.schedule.frequency,
    interval: rule.schedule.interval,
    dayOfMonth: rule.schedule.day_of_month ?? undefined,
    weekday: rule.schedule.weekday ?? undefined,
    month: rule.schedule.month ?? undefined,
  }
}

// ScheduleFields is the frequency/interval/day-of-month/weekday/month
// group, shared as-is between create and edit — the fields it shows
// depend only on the currently-selected frequency, never on isEdit
// (unlike account/category/starts_on above, a schedule stays fully
// editable after creation per PatchRecurringRuleRequest).
function ScheduleFields({
  schedule,
  onChange,
}: {
  schedule: ScheduleInput
  onChange: (patch: Partial<ScheduleInput>) => void
}) {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-4">
        <div className="flex flex-1 flex-col gap-1.5">
          <Label htmlFor="rule-frequency">Repeats</Label>
          <Select
            value={schedule.frequency}
            onValueChange={(value) =>
              onChange({ frequency: value as ScheduleInput['frequency'] })
            }
          >
            <SelectTrigger id="rule-frequency">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="weekly">Weekly</SelectItem>
              <SelectItem value="monthly">Monthly</SelectItem>
              <SelectItem value="yearly">Yearly</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div className="flex w-28 flex-col gap-1.5">
          <Label htmlFor="rule-interval">Every</Label>
          <Input
            id="rule-interval"
            type="number"
            min={1}
            required
            value={schedule.interval}
            onChange={(e) =>
              onChange({ interval: Math.max(1, Number(e.target.value) || 1) })
            }
          />
        </div>
      </div>

      {schedule.frequency === 'weekly' && (
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="rule-weekday">On</Label>
          <Select
            value={String(schedule.weekday ?? 1)}
            onValueChange={(value) => onChange({ weekday: Number(value) })}
          >
            <SelectTrigger id="rule-weekday">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {WEEKDAY_OPTIONS.map((w) => (
                <SelectItem key={w.value} value={String(w.value)}>
                  {w.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}

      {(schedule.frequency === 'monthly' ||
        schedule.frequency === 'yearly') && (
        <div className="flex flex-wrap gap-4">
          {schedule.frequency === 'yearly' && (
            <div className="flex flex-1 flex-col gap-1.5">
              <Label htmlFor="rule-month">Month</Label>
              <Select
                value={String(schedule.month ?? 1)}
                onValueChange={(value) => onChange({ month: Number(value) })}
              >
                <SelectTrigger id="rule-month">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {MONTH_OPTIONS.map((m) => (
                    <SelectItem key={m.value} value={String(m.value)}>
                      {m.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
          <div className="flex w-28 flex-col gap-1.5">
            <Label htmlFor="rule-day-of-month">Day</Label>
            <Input
              id="rule-day-of-month"
              type="number"
              min={1}
              max={31}
              required
              value={schedule.dayOfMonth ?? 1}
              onChange={(e) =>
                onChange({
                  dayOfMonth: Math.min(
                    31,
                    Math.max(1, Number(e.target.value) || 1),
                  ),
                })
              }
            />
          </div>
        </div>
      )}
      <p className="text-muted-foreground text-xs">
        A day past the end of a shorter month (e.g. the 31st in April) clamps to
        that month’s last day rather than being skipped.
      </p>
    </div>
  )
}

export function RecurringRuleDialog({
  open,
  onOpenChange,
  rule,
  accounts,
  categories,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  // null = create a new rule; otherwise edit this one.
  rule: RecurringRule | null
  accounts: Account[]
  categories: Category[]
  onSaved: () => void
}) {
  const isEdit = rule !== null

  const [accountId, setAccountId] = useState('')
  const [categoryId, setCategoryId] = useState('')
  const [description, setDescription] = useState(rule?.description ?? '')
  const [amount, setAmount] = useState(rule?.amount ?? '')
  const [schedule, setSchedule] = useState<ScheduleInput>(() =>
    scheduleFromRule(rule),
  )
  const [startsOn, setStartsOn] = useState(rule?.starts_on ?? '')
  const [endsOn, setEndsOn] = useState(rule?.ends_on ?? '')
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

  const existingAccountName = rule
    ? (accounts.find((a) => a.id === rule.account_id)?.name ?? rule.account_id)
    : ''
  const existingCategoryName = rule
    ? (categories.find((c) => c.id === rule.category_id)?.name ??
      rule.category_id)
    : ''

  function updateSchedule(patch: Partial<ScheduleInput>) {
    setSchedule((prev) => ({ ...prev, ...patch }))
  }

  const canSubmit =
    description.trim() !== '' &&
    amount.trim() !== '' &&
    (isEdit || (accountId !== '' && categoryId !== ''))

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canSubmit) return
    setSubmitting(true)
    try {
      if (isEdit) {
        await updateRecurringRule(rule.id, {
          amount: amount.trim(),
          description: description.trim(),
          schedule,
          endsOn: endsOn || undefined,
        })
        toast.success('Recurring rule saved.')
      } else {
        await createRecurringRule({
          accountRef: accountId,
          categoryRef: categoryId,
          amount: amount.trim(),
          description: description.trim(),
          schedule,
          startsOn: startsOn || undefined,
          endsOn: endsOn || undefined,
        })
        toast.success('Recurring rule created.')
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
          <DialogTitle>
            {isEdit ? 'Edit recurring rule' : 'New recurring rule'}
          </DialogTitle>
          <DialogDescription>
            {isEdit
              ? 'Change the amount, description, schedule, or end date.'
              : 'Plan a transaction that repeats on a schedule — nothing is recorded until you generate and materialise an occurrence.'}
          </DialogDescription>
        </DialogHeader>

        <form
          className="flex min-h-0 flex-col gap-4"
          onSubmit={handleSubmit}
          noValidate
        >
          <div className="no-scrollbar -mx-4 flex max-h-[70vh] flex-col gap-4 overflow-y-auto px-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rule-description">Description</Label>
              <Input
                id="rule-description"
                required
                autoFocus
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </div>

            <div className="flex flex-wrap gap-4">
              <div className="flex flex-1 flex-col gap-1.5">
                <Label htmlFor="rule-account">Account</Label>
                {isEdit ? (
                  <p className="text-sm">{existingAccountName}</p>
                ) : accounts.length === 0 ? (
                  <p className="text-muted-foreground text-sm">
                    Loading accounts…
                  </p>
                ) : (
                  <Select value={accountId} onValueChange={setAccountId}>
                    <SelectTrigger id="rule-account">
                      <SelectValue placeholder="Choose an account" />
                    </SelectTrigger>
                    <SelectContent>
                      {accounts.map((a) => (
                        <SelectItem key={a.id} value={a.id}>
                          {a.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </div>
              <div className="flex flex-1 flex-col gap-1.5">
                <Label htmlFor="rule-category">Category</Label>
                {isEdit ? (
                  <p className="text-sm">{existingCategoryName}</p>
                ) : (
                  <Combobox
                    id="rule-category"
                    options={categoryOptions}
                    value={categoryId}
                    onChange={setCategoryId}
                    placeholder="Choose a category"
                    searchPlaceholder="Search categories…"
                    emptyText="No matching categories."
                  />
                )}
              </div>
            </div>
            {!isEdit && (
              <p className="text-muted-foreground -mt-2 text-xs">
                An income category makes this a recurring inflow; an expense
                category makes it a recurring outflow. Both are fixed once the
                rule is created.
              </p>
            )}

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="rule-amount">Amount</Label>
              <Input
                id="rule-amount"
                required
                inputMode="decimal"
                placeholder="0.00"
                className="w-32"
                value={amount}
                onChange={(e) => setAmount(sanitizeAmountInput(e.target.value))}
              />
            </div>

            <div className="border-t pt-4">
              <ScheduleFields schedule={schedule} onChange={updateSchedule} />
            </div>

            <div className="flex flex-wrap gap-4 border-t pt-4">
              <div className="flex flex-1 flex-col gap-1.5">
                <Label htmlFor="rule-starts-on">Starts on</Label>
                {isEdit ? (
                  <p className="text-sm">{rule.starts_on} (can’t be changed)</p>
                ) : (
                  <DatePicker
                    id="rule-starts-on"
                    value={startsOn}
                    onChange={setStartsOn}
                  />
                )}
              </div>
              <div className="flex flex-1 flex-col gap-1.5">
                <Label htmlFor="rule-ends-on">Ends on</Label>
                <DatePicker
                  id="rule-ends-on"
                  value={endsOn}
                  onChange={setEndsOn}
                  placeholder="Runs indefinitely"
                />
              </div>
            </div>
          </div>

          <DialogFooter>
            <Button type="submit" disabled={!canSubmit || submitting}>
              {submitting ? 'Saving…' : isEdit ? 'Save' : 'Create rule'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
