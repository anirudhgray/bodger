// The transaction list screen (issue #61), replacing routes.tsx's
// placeholder for the /transactions route. Filters are the "occasional"
// progressive-disclosure layer (docs/ux-principles.md §4) — one deliberate
// step away behind a toggle, not shown by default. Edit and delete are
// both inline and unconfirmed, matching the CLI's own
// `transactions delete` (internal/surface/cli/transactions.go: "deleting
// is reversible in the database... confirmation is for genuinely
// hard-to-undo actions", ux-principles.md §5).
import { Receipt } from 'lucide-react'
import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type FormEvent,
} from 'react'
import { useLocation } from 'react-router-dom'

import {
  useTransactionDialog,
  useTransactionSaved,
} from '@/hooks/use-transaction-dialog'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Collapsible } from '@/components/ui/collapsible'
import { Combobox, type ComboboxOption } from '@/components/ui/combobox'
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
import { RateFetchPopover } from '@/components/RateFetchPopover'
import {
  RateAmountTrigger,
  RateProvenanceDetail,
  UnconvertedNote,
} from '@/components/RateProvenance'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { toast } from 'sonner'
import {
  ApiError,
  deleteTransaction,
  fetchFxRates,
  getFxRate,
  getReportingCurrency,
  listAccounts,
  listCategories,
  listTransactions,
  type Account,
  type Category,
  type FxRate,
  type Transaction,
  type TransactionKind,
  type TransactionListFilter,
} from '@/lib/api'
import { buildCategoryTree } from '@/lib/category-tree'
import { capitalize } from '@/lib/utils'

// RowConversion is one non-transfer transaction's reporting-currency
// equivalent (issue #145) — a per-row, per-date ConvertAmount lookup, not
// one shared rate for the whole page the way Balances' single "current"
// policy can get away with (docs/design-system.md's "Refresh popover"
// section: a transaction list spans many distinct historical dates, so
// there can be one rate per (pair, date) combination in view). Keyed by
// transaction id in the `conversions` map below.
type RowConversion =
  | { status: 'loading' }
  | { status: 'ready'; rate: FxRate }
  // Covers both "no rate available" (ApiError code not_found) and any
  // other lookup failure — either way the row is shown explicitly, never
  // silently dropped or left blank (ADR-0004's mixed-policy-aggregate-
  // forbidden rule, the same one Balances' own `unconverted` list serves).
  | { status: 'unconverted'; reason: string }

// needsConversion is true only for a foreign-currency, non-transfer
// transaction — a transfer's own cross-currency provenance is #141's
// stored implied rate, not this lookup, and a transaction already in the
// reporting currency has nothing to convert.
function needsConversion(t: Transaction, reportingCurrency: string): boolean {
  return t.type !== 'transfer' && t.currency !== reportingCurrency
}

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

// Radix's SelectItem (unlike a native <option> or Combobox's cmdk-based
// items) rejects an empty-string value outright — Select.Root reserves
// "" internally for "nothing selected." The account/type filters' "Any"
// option needs a real, non-empty sentinel instead, translated back to
// undefined in applyFilters below.
const ANY_FILTER_VALUE = 'any'

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

  // Issue #145: each foreign-currency (non-transfer) row's own reporting-
  // currency equivalent. `reportingCurrency` is null until it's loaded
  // (or the user hasn't set one, per ux-principles.md §4 — nothing below
  // renders without it, matching Balances' own single-currency gating).
  // `conversions` is keyed by transaction id — a page can carry rows with
  // many distinct booked dates, so this is one lookup per row, not one
  // shared rate for the whole page.
  const [reportingCurrency, setReportingCurrency] = useState<string | null>(
    null,
  )
  const [conversions, setConversions] = useState<Map<string, RowConversion>>(
    new Map(),
  )
  const [expandedTxId, setExpandedTxId] = useState<string | null>(null)
  const [backfillOpen, setBackfillOpen] = useState(false)
  const [backfillCurrencies, setBackfillCurrencies] = useState<Set<string>>(
    new Set(),
  )
  const [backfillFrom, setBackfillFrom] = useState('')
  const [backfillTo, setBackfillTo] = useState('')
  const [backfilling, setBackfilling] = useState(false)

  const accountsByID = new Map(accounts.map((a) => [a.id, a.name]))
  const categoriesByID = new Map(categories.map((c) => [c.id, c.name]))

  // Issue #106: the category filter shows the same parent_id hierarchy
  // Settings' own category list and TransactionDialog's picker do,
  // rather than a flat alphabetical dump — "Any" stands in for the old
  // <select>'s empty-value option, since it isn't itself a category.
  const categoryOptions = useMemo<ComboboxOption[]>(
    () => [
      { value: '', label: 'Any' },
      ...buildCategoryTree(categories).map(({ category, depth, path }) => ({
        value: category.id,
        label: category.name,
        depth,
        path,
      })),
    ],
    [categories],
  )

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
    // Best-effort only, matching Balances' own fetch: labels which
    // currency every row's own equivalent is computed against. A failure
    // here just means no per-row conversion UI renders (needsConversion
    // above requires it), not that the transaction list itself fails.
    getReportingCurrency()
      .then((result) => {
        if (result.is_set) setReportingCurrency(result.currency)
      })
      .catch(() => {})
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

  // loadRowConversions is a pure read (GET /api/v1/fx/rates via
  // getFxRate) — it never triggers a provider fetch itself, only the
  // backfill popover's confirm button does that. Each transaction's own
  // `date` (the date it's booked to) is what's sent as the
  // transaction_date policy's date — never "today" — so a row booked
  // months ago is evaluated for staleness/availability against its own
  // date, not whatever day happens to be current when the page loads.
  // See TransactionsList.test.tsx's regression test for exactly this
  // substitution.
  const loadRowConversions = useCallback(
    (targets: Transaction[], currency: string) => {
      if (targets.length === 0) return
      setConversions((prev) => {
        const next = new Map(prev)
        for (const t of targets) next.set(t.id, { status: 'loading' })
        return next
      })
      for (const t of targets) {
        getFxRate({
          from: t.currency,
          to: currency,
          amount: t.amount,
          policy: 'transaction_date',
          transactionDate: t.date,
        })
          .then((rate) => {
            setConversions((prev) =>
              new Map(prev).set(t.id, { status: 'ready', rate }),
            )
          })
          .catch((err: unknown) => {
            const reason =
              err instanceof ApiError
                ? err.message
                : 'Couldn’t check the exchange rate.'
            setConversions((prev) =>
              new Map(prev).set(t.id, { status: 'unconverted', reason }),
            )
          })
      }
    },
    [],
  )

  // Runs whenever the loaded page or the reporting currency changes,
  // resolving a conversion for any row that doesn't have one yet.
  // `conversions` is a dependency (not just read inside) so that clearing
  // an entry — on edit (below) or after a backfill — re-triggers a lookup
  // for exactly that row, the same "re-read the visible page" behaviour
  // issue #139's own refresh does for Balances.
  useEffect(() => {
    if (reportingCurrency === null) return
    const targets = transactions.filter(
      (t) => needsConversion(t, reportingCurrency) && !conversions.has(t.id),
    )
    loadRowConversions(targets, reportingCurrency)
  }, [transactions, reportingCurrency, conversions, loadRowConversions])

  function applyFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const account = form.get('account') as string
    const type = form.get('type') as string
    const next: TransactionListFilter = {
      account: account && account !== ANY_FILTER_VALUE ? account : undefined,
      category: (form.get('category') as string) || undefined,
      type:
        type && type !== ANY_FILTER_VALUE
          ? (type as TransactionKind)
          : undefined,
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
    // Deleting a transaction is a user-triggered action (docs/design-
    // system.md's "Toasts vs. inline messages"), so both outcomes report
    // via a toast — the row has no other pending affordance while this
    // is in flight, and on failure it just stays put. toast.promise
    // doesn't hand back the underlying promise (it returns a toast id),
    // so the real `deletion` promise is still awaited directly below for
    // control flow — both just observe the same promise.
    const deletion = deleteTransaction(id)
    toast.promise(deletion, {
      loading: 'Deleting transaction…',
      success: 'Transaction deleted.',
      error: (err) =>
        err instanceof ApiError
          ? err.message
          : 'Couldn’t delete that transaction. Try again.',
    })
    try {
      await deletion
      setTransactions((prev) => prev.filter((t) => t.id !== id))
      setConversions((prev) => {
        if (!prev.has(id)) return prev
        const next = new Map(prev)
        next.delete(id)
        return next
      })
    } catch {
      // Failure is already reported via the toast.promise `error` option
      // above — the row stays in the list, nothing further to do here.
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
          // The edited transaction's amount/currency/date may have
          // changed, so any cached conversion for it is potentially
          // stale — drop it so the effect above resolves it fresh, rather
          // than keeping a conversion computed against the pre-edit
          // values.
          setConversions((prev) => {
            if (!prev.has(event.transaction.id)) return prev
            const next = new Map(prev)
            next.delete(event.transaction.id)
            return next
          })
        } else {
          load(filter, false)
        }
      },
      [load, filter],
    ),
  )

  const hasActiveFilters = Object.values(filter).some(Boolean)

  // Every foreign currency actually in play across the currently loaded
  // page, excluding the reporting currency itself — the backfill
  // popover's own candidate list, matching Balances' candidatePairs.
  const inUseCurrencies = useMemo(() => {
    if (reportingCurrency === null) return []
    return Array.from(
      new Set(
        transactions
          .filter((t) => needsConversion(t, reportingCurrency))
          .map((t) => t.currency),
      ),
    ).sort()
  }, [transactions, reportingCurrency])

  function openBackfill(open: boolean) {
    setBackfillOpen(open)
    if (!open) return
    // Pre-check whichever currencies actually need it (stale or
    // unconverted) and default the range to the span of booked dates
    // among those rows — both just save the common case a click, same as
    // Balances' own openRefresh; every candidate stays selectable and the
    // range stays editable regardless.
    const needsRefresh = new Set<string>()
    let earliest: string | undefined
    let latest: string | undefined
    for (const t of transactions) {
      if (
        reportingCurrency === null ||
        !needsConversion(t, reportingCurrency)
      ) {
        continue
      }
      const conv = conversions.get(t.id)
      const stale = conv?.status === 'ready' && conv.rate.stale
      if (conv?.status === 'unconverted' || stale) {
        needsRefresh.add(t.currency)
        if (!earliest || t.date < earliest) earliest = t.date
        if (!latest || t.date > latest) latest = t.date
      }
    }
    setBackfillCurrencies(needsRefresh.size > 0 ? needsRefresh : new Set())
    setBackfillFrom(earliest ?? '')
    setBackfillTo(latest ?? '')
  }

  async function handleBackfillConfirm() {
    setBackfilling(true)
    try {
      await fetchFxRates(Array.from(backfillCurrencies), {
        from: backfillFrom,
        to: backfillTo,
      })
      toast.success('Rates backfilled.')
      setBackfillOpen(false)
      // Re-read the visible page (issue #145, matching #139's
      // re-read-after-refresh): drop the cached conversion for every row
      // in one of the backfilled currencies so the effect above resolves
      // it again, picking up whatever the backfill just stored.
      setConversions((prev) => {
        const next = new Map(prev)
        for (const t of transactions) {
          if (backfillCurrencies.has(t.currency)) next.delete(t.id)
        }
        return next
      })
    } catch (err) {
      toast.error(
        err instanceof ApiError
          ? err.message
          : 'Couldn’t backfill rates. Try again.',
      )
    } finally {
      setBackfilling(false)
    }
  }

  return (
    <div className="flex flex-1 flex-col gap-4 p-4 sm:p-6">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">Transactions</h1>
        <div className="flex items-center gap-2">
          {inUseCurrencies.length > 0 && (
            <RateFetchPopover
              open={backfillOpen}
              onOpenChange={openBackfill}
              triggerLabel="Backfill rates"
              title="Backfill rates"
              description="Fetch rates for the currencies and date range you pick."
              candidates={inUseCurrencies}
              selected={backfillCurrencies}
              onToggle={(currency, checked) => {
                setBackfillCurrencies((prev) => {
                  const next = new Set(prev)
                  if (checked) next.add(currency)
                  else next.delete(currency)
                  return next
                })
              }}
              onConfirm={handleBackfillConfirm}
              confirming={backfilling}
              confirmLabel="Backfill"
              dateRange={{
                from: backfillFrom,
                to: backfillTo,
                onFromChange: setBackfillFrom,
                onToChange: setBackfillTo,
              }}
            />
          )}
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
      </div>

      {showFilters && (
        <form
          key={formResetKey}
          onSubmit={applyFilters}
          className="border-border flex flex-wrap items-end gap-3 rounded-lg border p-3 sm:gap-4"
        >
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-account">Account</Label>
            <Select
              name="account"
              defaultValue={filter.account ?? ANY_FILTER_VALUE}
            >
              <SelectTrigger id="filter-account">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ANY_FILTER_VALUE}>Any</SelectItem>
                {accounts.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-category">Category</Label>
            <Combobox
              id="filter-category"
              name="category"
              defaultValue={filter.category ?? ''}
              options={categoryOptions}
              placeholder="Any"
              searchPlaceholder="Search categories…"
              emptyText="No matching categories."
              className="w-44"
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="filter-type">Type</Label>
            <Select name="type" defaultValue={filter.type ?? ANY_FILTER_VALUE}>
              <SelectTrigger id="filter-type">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ANY_FILTER_VALUE}>Any</SelectItem>
                {KIND_OPTIONS.map((k) => (
                  <SelectItem key={k.value} value={k.value}>
                    {k.label}
                  </SelectItem>
                ))}
              </SelectContent>
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
            {transactions.map((t) => {
              // Issue #145: this transaction's own reporting-currency
              // equivalent, if it needs one. `undefined` means either it
              // doesn't need one (transfer, or already in the reporting
              // currency) or the lookup just hasn't resolved yet — both
              // render nothing below, matching Balances' own "no flicker
              // while loading" behaviour.
              const conv =
                reportingCurrency !== null &&
                needsConversion(t, reportingCurrency)
                  ? conversions.get(t.id)
                  : undefined
              const expanded = expandedTxId === t.id

              return (
                <li key={t.id}>
                  <Collapsible
                    open={expanded}
                    onOpenChange={(open) => setExpandedTxId(open ? t.id : null)}
                  >
                    <div className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4 sm:py-2.5">
                      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                        <div className="flex items-center gap-2 text-sm">
                          <span className="text-muted-foreground">
                            {t.date}
                          </span>
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
                                    onClick={() =>
                                      filterByCategory(t.category_id!)
                                    }
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
                      <div className="flex items-center justify-between gap-3 sm:shrink-0 sm:justify-end sm:gap-4">
                        <div className="text-sm font-medium tabular-nums">
                          {t.type === 'inflow'
                            ? '+'
                            : t.type === 'outflow'
                              ? '−'
                              : ''}
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
                      </div>
                    </div>
                    {/* The conversion hint gets its own full-width row
                        rather than squeezing into the amount column above
                        — that column sits in a `shrink-0` flex item next
                        to the Edit/Delete buttons, which have no room to
                        spare for a potentially long "Not converted —
                        <reason>" message (issue #145 shows the reason
                        text as-is, not truncated). */}
                    {conv?.status === 'ready' && (
                      <div className="flex justify-end px-4 pb-2 sm:pb-2.5">
                        {/* rate.converted already carries its own currency
                            code ("9.26 USD") — internal/surface/http/fx.go's
                            fxRateViewFrom, same as FxConversionHint.tsx's own
                            note — appending rate.to here would render it
                            twice. */}
                        <RateAmountTrigger stale={conv.rate.stale}>
                          ≈ {conv.rate.converted}
                        </RateAmountTrigger>
                      </div>
                    )}
                    {conv?.status === 'unconverted' && (
                      <div className="flex justify-end px-4 pb-2 text-right sm:pb-2.5">
                        <UnconvertedNote reason={conv.reason} />
                      </div>
                    )}
                    {conv?.status === 'ready' && (
                      <RateProvenanceDetail>
                        1 {t.currency} = {conv.rate.rate} {conv.rate.to}
                        {' · as of '}
                        {conv.rate.rate_date}
                        {' · '}
                        {conv.rate.rate_source}
                        {conv.rate.stale && ' · stale'}
                      </RateProvenanceDetail>
                    )}
                  </Collapsible>
                </li>
              )
            })}
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
