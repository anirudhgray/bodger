// The transaction list screen (issue #61), replacing routes.tsx's
// placeholder for the /transactions route. Filters are the "occasional"
// progressive-disclosure layer (docs/ux-principles.md §4) — one deliberate
// step away behind a toggle, not shown by default. Edit and delete are
// both inline and unconfirmed, matching the CLI's own
// `transactions delete` (internal/surface/cli/transactions.go: "deleting
// is reversible in the database... confirmation is for genuinely
// hard-to-undo actions", ux-principles.md §5).
import { Receipt } from 'lucide-react'
import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { useLocation } from 'react-router-dom'

import {
  useTransactionDialog,
  useTransactionSaved,
} from '@/components/TransactionDialog'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { DatePicker } from '@/components/ui/date-picker'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import {
  ApiError,
  deleteTransaction,
  listAccounts,
  listCategories,
  listTransactions,
  type Account,
  type Category,
  type Transaction,
  type TransactionKind,
  type TransactionListFilter,
} from '@/lib/api'
import { capitalize } from '@/lib/utils'

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

// Title-cased for the filter dropdown — a discrete list of choices reads
// as Title Case (matching the account/category type dropdowns), unlike
// kindLabel's deliberately-lowercase inline sentence usage above.
const KIND_OPTIONS: { value: TransactionKind; label: string }[] = (
  ['outflow', 'inflow', 'transfer'] as const
).map((value) => ({ value, label: capitalize(kindLabel(value)) }))

function nameFor(id: string | undefined, byId: Map<string, string>): string {
  if (!id) return ''
  return byId.get(id) ?? id
}

export function TransactionsList() {
  // Cross-navigation (issue #89) hands an initial filter in via router
  // state — e.g. clicking an account on Balances — rather than a
  // shareable query-string, which is a separate, larger piece of work
  // (deep-linking every filter combination) this issue deliberately
  // doesn't take on.
  const location = useLocation()
  const initialFilter = (
    location.state as { filter?: TransactionListFilter } | null
  )?.filter

  const [transactions, setTransactions] = useState<Transaction[]>([])
  const [nextCursor, setNextCursor] = useState<string | undefined>()
  const [accounts, setAccounts] = useState<Account[]>([])
  const [categories, setCategories] = useState<Category[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showFilters, setShowFilters] = useState(Boolean(initialFilter))
  const [filter, setFilter] = useState<TransactionListFilter>(
    initialFilter ?? {},
  )
  // Bumped on Clear to remount the filter form below (key={formResetKey}):
  // its account/category/type/from/to fields are uncontrolled
  // (defaultValue={filter.x ?? ''}) so they read once from filter at mount
  // and never re-sync when filter state changes later, only when React
  // actually recreates the DOM nodes. A remount is what makes Clear visibly
  // clear the form, not just the data it's fetching with.
  const [formResetKey, setFormResetKey] = useState(0)
  const { openCreate, openEdit } = useTransactionDialog()

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
    Promise.allSettled([
      listAccounts().then(setAccounts),
      listCategories().then(setCategories),
    ]).then(() => {
      // The account/category <select>s' defaultValue is read once at
      // mount; if an initial filter (from cross-navigation, issue #89)
      // named an account/category whose <option> hadn't loaded yet, the
      // form never shows it as selected even though the fetch below used
      // it correctly. Remount the form once real options exist so
      // defaultValue is re-applied against them.
      if (initialFilter) setFormResetKey((k) => k + 1)
    })
    load(initialFilter ?? {}, false)
    // Runs once on mount only (load's own useCallback has an empty
    // dependency array, so it's stable) — applyFilters/clearFilters below
    // re-fetch directly from the event that changed the filter, rather
    // than this effect reacting to filter state (oxlint's
    // set-state-in-effect: derive from the event that caused the change,
    // don't loop through an effect). initialFilter is read once from
    // location.state on the initial render and intentionally left out of
    // the dependency array for the same reason.
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

  // The one other dead-ended intent the issue calls out by name: a
  // category shown on a transaction row currently does nothing when
  // clicked. Filters the same screen by that category rather than
  // navigating away, since there's nowhere else for "show me this
  // category's other transactions" to go.
  function filterByCategory(categoryId: string) {
    const next: TransactionListFilter = { category: categoryId }
    setFilter(next)
    setShowFilters(true)
    setFormResetKey((k) => k + 1)
    load(next, false)
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

  function handleEdit(transaction: Transaction) {
    openEdit(transaction)
  }

  // Fires for every create/edit, wherever it was triggered from — not
  // just one this screen's own openCreate()/openEdit() calls made. The
  // nav's global Add button has no reference to this screen's load() or
  // setTransactions at all, so without this, a transaction added while
  // already on this page would silently not appear until a manual
  // reload.
  useTransactionSaved(
    useCallback(
      (event) => {
        if (event.mode === 'edit') {
          setTransactions((prev) =>
            prev.map((t) =>
              t.id === event.transaction.id ? event.transaction : t,
            ),
          )
        } else {
          load(filter, false)
        }
      },
      [load, filter],
    ),
  )

  const hasActiveFilters = Object.values(filter).some(Boolean)

  return (
    <div className="flex flex-1 flex-col gap-4 p-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">Transactions</h1>
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
            <Select
              id="filter-account"
              name="account"
              defaultValue={filter.account ?? ''}
            >
              <option value="">Any</option>
              {accounts.map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
            </Select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-category">Category</Label>
            <Select
              id="filter-category"
              name="category"
              defaultValue={filter.category ?? ''}
            >
              <option value="">Any</option>
              {categories.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </Select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-type">Type</Label>
            <Select
              id="filter-type"
              name="type"
              defaultValue={filter.type ?? ''}
            >
              <option value="">Any</option>
              {KIND_OPTIONS.map((k) => (
                <option key={k.value} value={k.value}>
                  {k.label}
                </option>
              ))}
            </Select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-from">From</Label>
            <DatePicker
              id="filter-from"
              name="from"
              defaultValue={filter.from ?? ''}
              className="w-36"
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-to">To</Label>
            <DatePicker
              id="filter-to"
              name="to"
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
        <div className="flex items-center justify-center gap-2 p-12">
          <Spinner />
          <span className="text-muted-foreground text-sm">Loading…</span>
        </div>
      ) : transactions.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Receipt />
            </EmptyMedia>
            <EmptyTitle>
              {hasActiveFilters
                ? 'No matching transactions'
                : 'No transactions yet'}
            </EmptyTitle>
            <EmptyDescription>
              {hasActiveFilters
                ? 'Try adjusting or clearing your filters.'
                : 'Record your first transaction to see it here.'}
            </EmptyDescription>
          </EmptyHeader>
          {!hasActiveFilters && (
            <EmptyContent>
              <Button onClick={() => openCreate()}>Record a transaction</Button>
            </EmptyContent>
          )}
        </Empty>
      ) : (
        <Card className="[--card-spacing:0]">
          <ul className="divide-border flex flex-col divide-y">
            {transactions.map((t) => (
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
                    {t.type === 'transfer' ? (
                      <>
                        {' · '}
                        {nameFor(t.from_account_id, accountsByID)} →{' '}
                        {nameFor(t.to_account_id, accountsByID)}
                      </>
                    ) : (
                      <>
                        {' · '}
                        {nameFor(t.account_id, accountsByID)}
                        {t.category_id && (
                          <>
                            {' · '}
                            <button
                              type="button"
                              onClick={() => filterByCategory(t.category_id!)}
                              className="hover:text-foreground underline-offset-2 hover:underline"
                            >
                              {nameFor(t.category_id, categoriesByID)}
                            </button>
                          </>
                        )}
                      </>
                    )}
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
                    onClick={() => handleEdit(t)}
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
            ))}
          </ul>
        </Card>
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
