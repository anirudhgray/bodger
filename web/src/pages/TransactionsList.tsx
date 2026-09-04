// The transaction list screen (issue #61), replacing routes.tsx's
// placeholder for the /transactions route. Filters are the "occasional"
// progressive-disclosure layer (docs/ux-principles.md §4) — one deliberate
// step away behind a toggle, not shown by default. Edit and delete are
// both inline and unconfirmed, matching the CLI's own
// `transactions delete` (internal/surface/cli/transactions.go: "deleting
// is reversible in the database... confirmation is for genuinely
// hard-to-undo actions", ux-principles.md §5).
import { useCallback, useEffect, useState, type FormEvent } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'
import {
  ApiError,
  deleteTransaction,
  listAccounts,
  listCategories,
  listTransactions,
  updateTransaction,
  type Account,
  type Category,
  type EditTransactionBody,
  type Transaction,
  type TransactionKind,
  type TransactionListFilter,
} from '@/lib/api'

// kindLabel mirrors internal/surface/cli/transactions.go's
// transactionTypeFor: the same verb a transaction was recorded with
// (spend/receive/move), not the wire vocabulary — "one vocabulary, not
// two" across every surface a person reads (docs/ux-principles.md §7).
function kindLabel(kind: TransactionKind): string {
  switch (kind) {
    case 'outflow':
      return 'spend'
    case 'inflow':
      return 'receive'
    case 'transfer':
      return 'move'
  }
}

const KIND_OPTIONS: { value: TransactionKind; label: string }[] = [
  { value: 'outflow', label: 'spend' },
  { value: 'inflow', label: 'receive' },
  { value: 'transfer', label: 'move' },
]

function nameFor(id: string | undefined, byId: Map<string, string>): string {
  if (!id) return ''
  return byId.get(id) ?? id
}

const selectClassName = cn(
  'border-input bg-background flex h-8 w-full min-w-0 rounded-lg border px-2.5 text-sm shadow-xs',
  'focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 outline-none',
)

export function TransactionsList() {
  const [transactions, setTransactions] = useState<Transaction[]>([])
  const [nextCursor, setNextCursor] = useState<string | undefined>()
  const [accounts, setAccounts] = useState<Account[]>([])
  const [categories, setCategories] = useState<Category[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showFilters, setShowFilters] = useState(false)
  const [filter, setFilter] = useState<TransactionListFilter>({})
  // Bumped on Clear to remount the filter form below (key={formResetKey}):
  // its account/category/type/from/to fields are uncontrolled
  // (defaultValue={filter.x ?? ''}) so they read once from filter at mount
  // and never re-sync when filter state changes later, only when React
  // actually recreates the DOM nodes. A remount is what makes Clear visibly
  // clear the form, not just the data it's fetching with.
  const [formResetKey, setFormResetKey] = useState(0)
  const [editingID, setEditingID] = useState<string | null>(null)

  const accountsByID = new Map(accounts.map((a) => [a.id, a.name]))
  const categoriesByID = new Map(categories.map((c) => [c.id, c.name]))

  const load = useCallback(
    async (appliedFilter: TransactionListFilter, append: boolean) => {
      setLoading(true)
      setError(null)
      try {
        const page = await listTransactions(appliedFilter)
        setTransactions((prev) =>
          append ? [...prev, ...page.data] : page.data,
        )
        setNextCursor(page.next_cursor)
      } catch (err) {
        setError(
          err instanceof ApiError
            ? err.message
            : 'Couldn’t load transactions. Try again.',
        )
      } finally {
        setLoading(false)
      }
    },
    [],
  )

  useEffect(() => {
    listAccounts()
      .then(setAccounts)
      .catch(() => {})
    listCategories()
      .then(setCategories)
      .catch(() => {})
    load({}, false)
    // Runs once on mount only (load's own useCallback has an empty
    // dependency array, so it's stable) — applyFilters/clearFilters below
    // re-fetch directly from the event that changed the filter, rather
    // than this effect reacting to filter state (oxlint's
    // set-state-in-effect: derive from the event that caused the change,
    // don't loop through an effect).
  }, [load])

  function applyFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const next: TransactionListFilter = {
      account: (form.get('account') as string) || undefined,
      category: (form.get('category') as string) || undefined,
      type: (form.get('type') as TransactionKind) || undefined,
      from: (form.get('from') as string) || undefined,
      to: (form.get('to') as string) || undefined,
    }
    setFilter(next)
    load(next, false)
  }

  function clearFilters() {
    setFilter({})
    setFormResetKey((k) => k + 1)
    load({}, false)
  }

  async function handleDelete(id: string) {
    try {
      await deleteTransaction(id)
      setTransactions((prev) => prev.filter((t) => t.id !== id))
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : 'Couldn’t delete that transaction. Try again.',
      )
    }
  }

  async function handleSave(id: string, body: EditTransactionBody) {
    const updated = await updateTransaction(id, body)
    setTransactions((prev) => prev.map((t) => (t.id === id ? updated : t)))
    setEditingID(null)
  }

  const hasActiveFilters = Object.values(filter).some(Boolean)

  return (
    <div className="flex flex-col gap-4 p-6">
      <div className="flex items-center justify-between">
        <h1 className="text-lg font-semibold tracking-tight">Transactions</h1>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => setShowFilters((v) => !v)}
        >
          {showFilters ? 'Hide filters' : 'Filters'}
          {hasActiveFilters ? ' •' : ''}
        </Button>
      </div>

      {showFilters && (
        <form
          key={formResetKey}
          onSubmit={applyFilters}
          className="border-border flex flex-wrap items-end gap-3 rounded-lg border p-3"
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-account">Account</Label>
            <select
              id="filter-account"
              name="account"
              defaultValue={filter.account ?? ''}
              className={selectClassName}
            >
              <option value="">Any</option>
              {accounts.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-category">Category</Label>
            <select
              id="filter-category"
              name="category"
              defaultValue={filter.category ?? ''}
              className={selectClassName}
            >
              <option value="">Any</option>
              {categories.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-type">Type</Label>
            <select
              id="filter-type"
              name="type"
              defaultValue={filter.type ?? ''}
              className={selectClassName}
            >
              <option value="">Any</option>
              {KIND_OPTIONS.map((k) => (
                <option key={k.value} value={k.value}>
                  {k.label}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-from">From</Label>
            <Input
              id="filter-from"
              name="from"
              type="date"
              defaultValue={filter.from ?? ''}
              className="w-36"
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-to">To</Label>
            <Input
              id="filter-to"
              name="to"
              type="date"
              defaultValue={filter.to ?? ''}
              className="w-36"
            />
          </div>
          <div className="flex gap-2">
            <Button type="submit" size="sm">
              Apply
            </Button>
            {hasActiveFilters && (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={clearFilters}
              >
                Clear
              </Button>
            )}
          </div>
        </form>
      )}

      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}

      {loading && transactions.length === 0 ? (
        <p className="text-muted-foreground text-sm">Loading…</p>
      ) : transactions.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No transactions {hasActiveFilters ? 'match those filters' : 'yet'}.
        </p>
      ) : (
        <ul className="border-border divide-border flex flex-col divide-y rounded-lg border">
          {transactions.map((t) =>
            editingID === t.id ? (
              <EditRow
                key={t.id}
                transaction={t}
                accounts={accounts}
                categories={categories}
                onCancel={() => setEditingID(null)}
                onSave={(body) => handleSave(t.id, body)}
              />
            ) : (
              <li
                key={t.id}
                className="flex items-center justify-between gap-4 px-4 py-2.5"
              >
                <div className="flex flex-1 flex-col gap-0.5">
                  <div className="flex items-center gap-2 text-sm">
                    <span className="text-muted-foreground">{t.date}</span>
                    <span className="font-medium">{t.description}</span>
                  </div>
                  <div className="text-muted-foreground text-xs">
                    {kindLabel(t.type)}
                    {t.type === 'transfer'
                      ? ` · ${nameFor(t.from_account_id, accountsByID)} → ${nameFor(t.to_account_id, accountsByID)}`
                      : ` · ${nameFor(t.account_id, accountsByID)}${t.category_id ? ` · ${nameFor(t.category_id, categoriesByID)}` : ''}`}
                  </div>
                </div>
                <div className="text-sm font-medium tabular-nums">
                  {t.type === 'inflow' ? '+' : t.type === 'outflow' ? '−' : ''}
                  {t.amount} {t.currency}
                </div>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => setEditingID(t.id)}
                  >
                    Edit
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => handleDelete(t.id)}
                  >
                    Delete
                  </Button>
                </div>
              </li>
            ),
          )}
        </ul>
      )}

      {nextCursor && !loading && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => load({ ...filter, cursor: nextCursor }, true)}
          className="self-start"
        >
          Load more
        </Button>
      )}
    </div>
  )
}

// EditRow replaces one list row with a form pre-filled from transaction —
// api.ts's updateTransaction is a full replacement (see EditTransactionBody's
// doc comment), so every field is submitted back, changed or not.
function EditRow({
  transaction,
  accounts,
  categories,
  onCancel,
  onSave,
}: {
  transaction: Transaction
  accounts: Account[]
  categories: Category[]
  onCancel: () => void
  onSave: (body: EditTransactionBody) => Promise<void>
}) {
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const isTransfer = transaction.type === 'transfer'

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setSaving(true)
    const form = new FormData(event.currentTarget)
    const body: EditTransactionBody = {
      amount: form.get('amount') as string,
      description: form.get('description') as string,
      date: (form.get('date') as string) || undefined,
      notes: (form.get('notes') as string) || undefined,
      ...(isTransfer
        ? {
            from_account: form.get('from_account') as string,
            to_account: form.get('to_account') as string,
          }
        : {
            account: form.get('account') as string,
            category: (form.get('category') as string) || undefined,
          }),
    }
    try {
      await onSave(body)
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : 'Couldn’t save that change. Try again.',
      )
      setSaving(false)
    }
  }

  return (
    <li className="px-4 py-3">
      <form
        onSubmit={handleSubmit}
        aria-label={`Edit ${transaction.description}`}
        className="flex flex-wrap items-end gap-3"
      >
        <div className="flex flex-col gap-1">
          <Label htmlFor={`date-${transaction.id}`}>Date</Label>
          <Input
            id={`date-${transaction.id}`}
            name="date"
            type="date"
            defaultValue={transaction.date}
            className="w-36"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor={`description-${transaction.id}`}>Description</Label>
          <Input
            id={`description-${transaction.id}`}
            name="description"
            defaultValue={transaction.description}
            required
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor={`amount-${transaction.id}`}>Amount</Label>
          <Input
            id={`amount-${transaction.id}`}
            name="amount"
            defaultValue={transaction.amount}
            required
            className="w-28"
          />
        </div>
        {isTransfer ? (
          <>
            <div className="flex flex-col gap-1">
              <Label htmlFor={`from-${transaction.id}`}>From</Label>
              <select
                id={`from-${transaction.id}`}
                name="from_account"
                defaultValue={transaction.from_account_id}
                className={selectClassName}
              >
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor={`to-${transaction.id}`}>To</Label>
              <select
                id={`to-${transaction.id}`}
                name="to_account"
                defaultValue={transaction.to_account_id}
                className={selectClassName}
              >
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            </div>
          </>
        ) : (
          <>
            <div className="flex flex-col gap-1">
              <Label htmlFor={`account-${transaction.id}`}>Account</Label>
              <select
                id={`account-${transaction.id}`}
                name="account"
                defaultValue={transaction.account_id}
                className={selectClassName}
              >
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor={`category-${transaction.id}`}>Category</Label>
              <select
                id={`category-${transaction.id}`}
                name="category"
                defaultValue={transaction.category_id ?? ''}
                className={selectClassName}
              >
                <option value="">None</option>
                {categories.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </div>
          </>
        )}
        <div className="flex gap-2">
          <Button type="submit" size="sm" disabled={saving}>
            {saving ? 'Saving…' : 'Save'}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onCancel}
            disabled={saving}
          >
            Cancel
          </Button>
        </div>
        {error && (
          <p role="alert" className="text-destructive w-full text-sm">
            {error}
          </p>
        )}
      </form>
    </li>
  )
}
