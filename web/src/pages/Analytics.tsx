// The analytics screen (issue #189): M5's visible payoff — charts for
// spending/income by category, cash flow, trends, and savings rate, all
// rendered from the four GET /api/v1/analytics/* endpoints (issue #188)
// exactly as the application layer computed them. This screen must never
// sum, convert, or bucket anything itself (ADR-0009's "the web UI does no
// maths" rule) — every number plotted below is a server-computed decimal
// string, only ever parsed to a plain JS number for a chart's y-axis
// position (never combined with another figure, never re-derived).
//
// Filter chrome is deliberately narrow — a date range and the reporting
// currency, per the issue's own scope — not the full ADR-0009 filter
// dimensions the API/CLI surfaces already accept. Dates default to empty
// (an unbounded "all time" query) rather than a client-computed "today"
// or "N months ago": docs/architecture.md's normalize-once rule reserves
// resolving "now" for the application layer, and TransactionsList's own
// filter chrome already established leaving date-range inputs blank by
// default rather than guessing a client-side window.
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  BarChart3,
  TrendingDown,
  TrendingUp,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { toast } from 'sonner'

import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Label as RechartsLabel,
  Pie,
  PieChart,
  XAxis,
  YAxis,
} from 'recharts'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  ChartContainer,
  ChartLegend,
  ChartLegendContent,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@/components/ui/chart'
import { DatePicker } from '@/components/ui/date-picker'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Label } from '@/components/ui/label'
import { RateFetchPopover } from '@/components/RateFetchPopover'
import { UnconvertedNote } from '@/components/RateProvenance'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  ApiError,
  fetchFxRates,
  getAverageTransactionSize,
  getCashFlow,
  getCategoryBreakdown,
  getCategoryTrends,
  getReportingCurrency,
  getSavingsRate,
  getTopTransactions,
  getTrends,
  listAccounts,
  type AverageTransactionSize,
  type CashFlow,
  type CategoryBreakdown,
  type CategoryTrends,
  type Granularity,
  type SavingsRate,
  type TopTransactions,
  type Trends,
} from '@/lib/api'

function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : 'Couldn’t reach the server. Try again.'
}

// Per-category pie-slice colors cycle through the same five chart tokens
// index.css defines (docs/design-system.md's "Charts" section) — there's
// no fixed identity between a category and a hue the way inflow/outflow
// have with success/destructive, so this just assigns them in row order.
const CATEGORY_COLORS = [
  'var(--chart-1)',
  'var(--chart-2)',
  'var(--chart-3)',
  'var(--chart-4)',
  'var(--chart-5)',
]

type CategorySlice = { category: string; value: number; fill: string }

// categoryPieData/categoryPieConfig build a pie's slices (and the
// ChartConfig ChartTooltipContent/ChartLegendContent key off) from the
// already-positive-filtered spendingRows/incomeRows below — one call per
// donut, assigning each row a color in order.
function categoryPieData(
  rows: { category: string; amount: number }[],
): CategorySlice[] {
  return rows.map((r, i) => ({
    category: r.category,
    value: r.amount,
    fill: CATEGORY_COLORS[i % CATEGORY_COLORS.length],
  }))
}

function categoryPieConfig(slices: CategorySlice[]): ChartConfig {
  const config: ChartConfig = {}
  for (const s of slices)
    config[s.category] = { label: s.category, color: s.fill }
  return config
}

const cashFlowChartConfig: ChartConfig = {
  inflow: { label: 'Inflow', color: 'var(--chart-1)' },
  outflow: { label: 'Outflow', color: 'var(--chart-2)' },
}

// cashFlowSpan spans the earliest From through the latest To across
// CashFlow's own points — the refresh popover's fallback when the
// page's own date filter is empty, so a backfill fetch still targets the
// dates the query actually touched rather than defaulting to "today".
// Granularity-agnostic (issue #194): every point carries its own From/To
// regardless of bucket size, so this needs no month-specific parsing the
// way it did when a point was only ever a "YYYY-MM" string.
function cashFlowSpan(
  points: { from: string; to: string }[],
): { from: string; to: string } | null {
  if (points.length === 0) return null
  const froms = points.map((p) => p.from).sort()
  const tos = points.map((p) => p.to).sort()
  return { from: froms[0], to: tos[tos.length - 1] }
}

// formatPeriodLabel renders one CashFlowPoint's/TrendPeriod's [from, to]
// as a short, granularity-appropriate label — display formatting of
// dates the server already computed, not a new value (ADR-0009's "no
// client-side maths" rule is about deriving figures, not about choosing
// how to print an existing date).
function formatPeriodLabel(
  from: string,
  to: string,
  granularity: Granularity,
): string {
  const fromDate = new Date(`${from}T00:00:00`)
  const toDate = new Date(`${to}T00:00:00`)
  switch (granularity) {
    case 'year':
      return String(fromDate.getFullYear())
    case 'week': {
      const fromLabel = fromDate.toLocaleDateString('en-US', {
        month: 'short',
        day: 'numeric',
      })
      const toLabel =
        toDate.getMonth() === fromDate.getMonth()
          ? String(toDate.getDate())
          : toDate.toLocaleDateString('en-US', {
              month: 'short',
              day: 'numeric',
            })
      return `${fromLabel}–${toLabel}`
    }
    case 'custom':
      return `${from} – ${to}`
    default: // month
      return fromDate.toLocaleDateString('en-US', {
        month: 'short',
        year: 'numeric',
      })
  }
}

// periodPhrase names the current/previous Trends period in prose that
// matches the selected granularity ("This week"/"This month"/"This
// year", or "Current period"/"Previous period" for a custom range,
// which has no fixed cadence to name).
function periodPhrase(
  granularity: Granularity,
  which: 'current' | 'previous',
): string {
  if (granularity === 'custom')
    return which === 'current' ? 'Current period' : 'Previous period'
  const unit =
    granularity === 'week' ? 'week' : granularity === 'year' ? 'year' : 'month'
  return which === 'current' ? `This ${unit}` : `Last ${unit}`
}

// previousPeriodPhrase names what the current period's change percentages
// compare against, matching periodPhrase's own granularity-aware naming.
function previousPeriodPhrase(granularity: Granularity): string {
  if (granularity === 'custom') return 'the previous period'
  const unit =
    granularity === 'week' ? 'week' : granularity === 'year' ? 'year' : 'month'
  return `last ${unit}`
}

// changeLabel renders a nil-safe percentage change (Trends' own
// inflow_change_pct/outflow_change_pct are absent, not zero, when the
// previous period had nothing to compare against — ADR-0009's "no
// meaningful number here" case, rendered as such rather than as 0%).
function changeLabel(pct: number | null | undefined): string {
  if (pct === null || pct === undefined) return 'n/a'
  const sign = pct > 0 ? '+' : ''
  return `${sign}${pct.toFixed(1)}%`
}

// CategoryDonut is one of the two "Spending"/"Income" donuts — a plain
// icon sits in the hole rather than a computed total: a sum-of-slices
// figure is exactly the kind of client-side aggregation ADR-0009's "the
// web UI does no maths" rule exists to rule out (every number on this
// screen must trace back to a server-computed figure, not a page-local
// sum that could drift from what the CLI/API would report), so the
// center stays a static, non-numeric affordance instead.
//
// The icon in the donut hole is a recharts <Label content> render prop —
// shadcn's own "donut chart with text" example shape (a <tspan> centered
// on viewBox.cx/cy), swapping the centered text for a centered icon —
// rather than a CSS-positioned overlay div. Tying the icon's position to
// the same viewBox recharts computes for the label means it stays
// correctly centered through any resize/re-layout without a second,
// separately-maintained positioning system to keep in sync.
function DonutCenterIcon({
  icon: Icon,
  viewBox,
}: {
  icon: typeof TrendingUp
  viewBox?: { cx?: number; cy?: number }
}) {
  if (!viewBox?.cx || !viewBox?.cy) return null
  const size = 24
  return (
    <Icon
      x={viewBox.cx - size / 2}
      y={viewBox.cy - size / 2}
      width={size}
      height={size}
      className="text-muted-foreground"
      aria-hidden="true"
    />
  )
}

function CategoryDonut({
  title,
  icon,
  slices,
}: {
  title: string
  icon: typeof TrendingUp
  slices: CategorySlice[]
}) {
  return (
    <div>
      <p className="text-muted-foreground mb-2 text-sm font-medium">{title}</p>
      <ChartContainer
        config={categoryPieConfig(slices)}
        className="aspect-auto h-64 w-full"
      >
        <PieChart>
          <ChartTooltip content={<ChartTooltipContent />} />
          <Pie
            data={slices}
            dataKey="value"
            nameKey="category"
            innerRadius={50}
            outerRadius={80}
          >
            {slices.map((s) => (
              <Cell key={s.category} fill={s.fill} />
            ))}
            <RechartsLabel
              content={({ viewBox }) => (
                <DonutCenterIcon
                  icon={icon}
                  viewBox={viewBox as { cx?: number; cy?: number }}
                />
              )}
            />
          </Pie>
        </PieChart>
      </ChartContainer>
      {/* A recharts <ChartLegend> as a PieChart child (the shadcn default)
          makes recharts reserve internal bottom margin for it, which
          shrinks/re-centers the Pie — but the viewBox recharts hands to
          the <Label content> render prop above stays the *unadjusted*
          container center, not the Pie's own shifted one. That desync
          rendered the center icon visibly off the true circle's center.
          Rendering the legend as a plain list here instead, straight from
          the same `slices` the Pie already draws, keeps the Pie filling
          its full container (nothing to leave room for) so the two
          centers coincide. */}
      <div className="mt-3 flex flex-wrap items-center justify-center gap-4">
        {slices.map((s) => (
          <div key={s.category} className="flex items-center gap-1.5">
            <div
              className="h-2 w-2 shrink-0 rounded-[2px]"
              style={{ backgroundColor: s.fill }}
            />
            {s.category}
          </div>
        ))}
      </div>
    </div>
  )
}

// SortableHead is the category table's column header: click to sort by
// that column, click again to flip direction. Shows a neutral icon for
// every other column and the active direction's icon for the current
// sort key, so the table always communicates which column and direction
// it's currently sorted by.
function SortableHead({
  label,
  active,
  dir,
  onClick,
  align = 'left',
}: {
  label: string
  active: boolean
  dir: 'asc' | 'desc'
  onClick: () => void
  align?: 'left' | 'right'
}) {
  const Icon = active ? (dir === 'asc' ? ArrowUp : ArrowDown) : ArrowUpDown
  return (
    <TableHead className={align === 'right' ? 'text-right' : undefined}>
      <button
        type="button"
        onClick={onClick}
        className={`hover:text-foreground flex items-center gap-1 ${align === 'right' ? 'ml-auto' : ''}`}
      >
        {label}
        <Icon className="size-3.5" />
      </button>
    </TableHead>
  )
}

// CategoryTable is CategoryDonut's paired table — the same rows the
// donut charts, as a sortable Category/Amount table rather than a shape
// (issue #189's own scope note). One instance per donut, each with its
// own independent sort state, rather than one shared Spending/Income/Net
// table: a category is exclusively expense- or income-type in the domain
// model, so a combined table would have one of its two money columns at
// zero on nearly every row. Sorting only ever reorders rows the API
// already returned — never a re-fetch, never a derived figure.
function CategoryTable({
  rows,
  amountLabel,
}: {
  rows: { category: string; amount: number; amountRaw: string }[]
  amountLabel: string
}) {
  const [sortKey, setSortKey] = useState<'category' | 'amount'>('category')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')

  function toggleSort(key: typeof sortKey) {
    if (key === sortKey) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortKey(key)
      setSortDir('asc')
    }
  }

  const sorted = useMemo(() => {
    const sign = sortDir === 'asc' ? 1 : -1
    return [...rows].sort((a, b) =>
      sortKey === 'category'
        ? sign * a.category.localeCompare(b.category)
        : sign * (a.amount - b.amount),
    )
  }, [rows, sortKey, sortDir])

  return (
    <Table className="mt-4">
      <TableHeader>
        <TableRow>
          <SortableHead
            label="Category"
            active={sortKey === 'category'}
            dir={sortDir}
            onClick={() => toggleSort('category')}
          />
          <SortableHead
            label={amountLabel}
            active={sortKey === 'amount'}
            dir={sortDir}
            onClick={() => toggleSort('amount')}
            align="right"
          />
        </TableRow>
      </TableHeader>
      <TableBody>
        {sorted.map((row) => (
          <TableRow key={row.category}>
            <TableCell>{row.category}</TableCell>
            <TableCell className="text-right tabular-nums">
              {row.amountRaw}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

export function AnalyticsPage() {
  const [currency, setCurrency] = useState('')
  const [currencies, setCurrencies] = useState<string[]>([])
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  // Defaults to "month" (issue #194) so nothing about the page's default
  // rendering changes for anyone who never touches this selector.
  const [granularity, setGranularity] = useState<Granularity>('month')

  const [breakdown, setBreakdown] = useState<CategoryBreakdown | null>(null)
  const [cashFlow, setCashFlow] = useState<CashFlow | null>(null)
  const [trends, setTrends] = useState<Trends | null>(null)
  const [savingsRate, setSavingsRate] = useState<SavingsRate | null>(null)
  // Issue #196's additional stats: top transactions, average transaction
  // size, and per-category trend deltas — fetched alongside M5's
  // baseline four, same filter/currency/granularity chrome.
  const [topTransactions, setTopTransactions] =
    useState<TopTransactions | null>(null)
  const [averageSize, setAverageSize] = useState<AverageTransactionSize | null>(
    null,
  )
  const [categoryTrends, setCategoryTrends] = useState<CategoryTrends | null>(
    null,
  )
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [refreshOpen, setRefreshOpen] = useState(false)
  const [refreshCurrencies, setRefreshCurrencies] = useState<Set<string>>(
    new Set(),
  )
  // Separate from the page's own from/to filter, same as TransactionsList's
  // backfillFrom/backfillTo — this screen always converts at the
  // transaction_date policy, so a "today only" fetch (Balances' own
  // popover shape) would store a rate outside every historical posting's
  // own within-7-days lookup window and silently do nothing useful.
  // Defaults to the page's own filter dates when set, else the span of
  // months cash flow actually returned (this screen has no per-posting
  // date to key off, unlike TransactionsList's real transaction rows) —
  // always left editable regardless, same as every other popover default.
  const [refreshFrom, setRefreshFrom] = useState('')
  const [refreshTo, setRefreshTo] = useState('')
  const [refreshing, setRefreshing] = useState(false)

  // Best-effort: this only seeds the currency selector's initial value
  // and options. A failure here doesn't block the rest of the screen —
  // the selector just starts empty, same as TransactionsList/Balances'
  // own reporting-currency lookups.
  useEffect(() => {
    let cancelled = false
    getReportingCurrency()
      .then((result) => {
        if (!cancelled) setCurrency(result.effectiveCurrency)
      })
      .catch(() => {})
    listAccounts()
      .then((accounts) => {
        if (cancelled) return
        setCurrencies(
          Array.from(new Set(accounts.map((a) => a.currency))).sort(),
        )
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  // A custom granularity needs both bounds to define CashFlow's single
  // bucket and Trends' current period (app.CashFlow/Trends both reject
  // an open-ended custom range as invalid input) — checked client-side
  // so an incomplete selection shows a plain hint instead of a server
  // error round-trip.
  const customRangeIncomplete = granularity === 'custom' && (!from || !to)

  const load = useCallback(() => {
    if (!currency) return
    if (customRangeIncomplete) {
      setBreakdown(null)
      setCashFlow(null)
      setTrends(null)
      setSavingsRate(null)
      setTopTransactions(null)
      setAverageSize(null)
      setCategoryTrends(null)
      setError(null)
      setLoading(false)
      return
    }
    setLoading(true)
    setError(null)
    const filter = { from: from || undefined, to: to || undefined }
    const options = { currency, policy: 'transaction_date' as const }
    Promise.all([
      getCategoryBreakdown(filter, options),
      getCashFlow(filter, options, granularity),
      getTrends(options, granularity, filter),
      getSavingsRate(filter, options),
      getTopTransactions(filter, options),
      getAverageTransactionSize(filter, options),
      getCategoryTrends(filter, options, granularity),
    ])
      .then(([b, cf, t, sr, top, avg, catTrends]) => {
        setBreakdown(b)
        setCashFlow(cf)
        setTrends(t)
        setSavingsRate(sr)
        setTopTransactions(top)
        setAverageSize(avg)
        setCategoryTrends(catTrends)
      })
      .catch((err: unknown) => setError(errorMessage(err)))
      .finally(() => setLoading(false))
  }, [currency, from, to, granularity, customRangeIncomplete])

  useEffect(() => {
    // load() fetches from the analytics API (an external system) whenever
    // currency/from/to change — not a value derivable during render.
    // oxlint-disable-next-line react/set-state-in-effect
    load()
  }, [load])

  // Every currency any of the four results flagged as unconverted — the
  // popover's candidate list. Deduplicated across all four since a
  // missing rate for, say, JPY affects every metric that touched a JPY
  // posting the same way.
  const unconvertedCurrencies = useMemo(() => {
    const set = new Set<string>()
    for (const result of [
      breakdown,
      cashFlow,
      trends,
      savingsRate,
      topTransactions,
      averageSize,
      categoryTrends,
    ]) {
      for (const u of result?.unconverted ?? []) set.add(u.currency)
    }
    return Array.from(set).sort()
  }, [
    breakdown,
    cashFlow,
    trends,
    savingsRate,
    topTransactions,
    averageSize,
    categoryTrends,
  ])

  function openRefresh(open: boolean) {
    setRefreshOpen(open)
    if (!open) return
    setRefreshCurrencies(new Set(unconvertedCurrencies))
    if (from && to) {
      setRefreshFrom(from)
      setRefreshTo(to)
      return
    }
    const span = cashFlowSpan(cashFlow?.points ?? [])
    setRefreshFrom(span?.from ?? from)
    setRefreshTo(span?.to ?? to)
  }

  async function handleRefresh() {
    setRefreshing(true)
    try {
      const range =
        refreshFrom && refreshTo
          ? { from: refreshFrom, to: refreshTo }
          : undefined
      await fetchFxRates(Array.from(refreshCurrencies), range, currency)
      toast.success('Rates refreshed.')
      setRefreshOpen(false)
      load()
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setRefreshing(false)
    }
  }

  // Numeric fields drive sorting and the pie charts' slice sizes; the
  // *Raw string fields are what the table actually displays — never
  // toFixed()'d, since that silently assumes every currency uses 2
  // decimal places (JPY uses 0, some use 3) where the server's own
  // AmountString already got this right.
  const categoryData = useMemo(
    () =>
      (breakdown?.rows ?? []).map((row) => ({
        category: row.category,
        spending: Number(row.spending),
        spendingRaw: row.spending,
        income: Number(row.income),
        incomeRaw: row.income,
      })),
    [breakdown],
  )

  // Split by type rather than one combined table: a category is
  // exclusively expense- or income-type in the domain model (issue #186's
  // Category), so a shared Spending/Income/Net table would have one of
  // its two money columns at zero on nearly every row. Two single-metric
  // tables, each paired with its own donut, match the data's actual
  // shape instead of forcing an artificial combined one.
  const spendingRows = useMemo(
    () =>
      categoryData
        .filter((r) => r.spending > 0)
        .map((r) => ({
          category: r.category,
          amount: r.spending,
          amountRaw: r.spendingRaw,
        })),
    [categoryData],
  )
  const incomeRows = useMemo(
    () =>
      categoryData
        .filter((r) => r.income > 0)
        .map((r) => ({
          category: r.category,
          amount: r.income,
          amountRaw: r.incomeRaw,
        })),
    [categoryData],
  )

  const spendingSlices = useMemo(
    () => categoryPieData(spendingRows),
    [spendingRows],
  )
  const incomeSlices = useMemo(() => categoryPieData(incomeRows), [incomeRows])

  const cashFlowData = useMemo(
    () =>
      (cashFlow?.points ?? []).map((point) => ({
        label: formatPeriodLabel(point.from, point.to, granularity),
        inflow: Number(point.inflow),
        outflow: Number(point.outflow),
      })),
    [cashFlow, granularity],
  )

  const hasAnyData =
    categoryData.length > 0 ||
    cashFlowData.length > 0 ||
    (trends !== null &&
      (trends.current.inflow !== '0' || trends.current.outflow !== '0')) ||
    (topTransactions?.rows.length ?? 0) > 0 ||
    (averageSize?.overall.count ?? 0) > 0 ||
    (categoryTrends?.rows.length ?? 0) > 0

  return (
    <div className="flex flex-1 flex-col gap-6 p-4 sm:p-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Analytics</h1>
        <p className="text-muted-foreground text-sm">
          Spending, income, cash flow, and savings — computed the same way
          across the CLI, API, and this screen.
        </p>
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1">
          <Label htmlFor="analytics-from">From</Label>
          <DatePicker id="analytics-from" value={from} onChange={setFrom} />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="analytics-to">To</Label>
          <DatePicker id="analytics-to" value={to} onChange={setTo} />
        </div>
        {/* Granularity selector (issue #194): defaults to "month" so the
            page's default rendering is unchanged. Drives both CashFlow's
            bucket size and Trends' comparison period — see
            app.Granularity's own doc comment for what each value means
            server-side. From/To above stay enabled for every granularity
            since CashFlow always uses them as its overall range
            regardless of bucket size; only Trends ignores them, and only
            for week/month/year. */}
        <div className="flex flex-col gap-1">
          <Label
            htmlFor="analytics-granularity"
            className="text-muted-foreground text-sm font-normal"
          >
            Granularity
          </Label>
          <Select
            value={granularity}
            onValueChange={(v) => setGranularity(v as Granularity)}
          >
            <SelectTrigger id="analytics-granularity" className="w-28">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="week">Week</SelectItem>
              <SelectItem value="month">Month</SelectItem>
              <SelectItem value="year">Year</SelectItem>
              <SelectItem value="custom">Custom</SelectItem>
            </SelectContent>
          </Select>
        </div>
        {/* Multi-currency chrome only appears once a second currency is
            actually in play — docs/ux-principles.md §4's progressive-
            disclosure rule (multi-currency policy is a "Power" layer
            concern), the same gate Balances.tsx applies to its own
            currency selector. A single-currency ledger never meets a
            currency picker it has no use for; `currency` still holds the
            resolved reporting currency underneath for the API calls. */}
        {currencies.length > 1 && (
          <div className="flex flex-col gap-1">
            <Label
              htmlFor="analytics-currency"
              className="text-muted-foreground text-sm font-normal"
            >
              Currency
            </Label>
            <Select value={currency} onValueChange={setCurrency}>
              <SelectTrigger id="analytics-currency" className="w-28">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {/* Every currency any account uses, plus the effective
                    reporting currency itself even if no account happens
                    to use it yet — mirrors Balances' own "Show in"
                    selector derivation. */}
                {Array.from(
                  new Set(currency ? [currency, ...currencies] : currencies),
                ).map((c) => (
                  <SelectItem key={c} value={c}>
                    {c}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        )}
        {unconvertedCurrencies.length > 0 && (
          <RateFetchPopover
            open={refreshOpen}
            onOpenChange={openRefresh}
            triggerLabel="Refresh rates"
            title="Refresh exchange rates"
            description="Fetch rates for the currencies this view couldn't convert."
            candidates={unconvertedCurrencies}
            selected={refreshCurrencies}
            onToggle={(c, checked) =>
              setRefreshCurrencies((prev) => {
                const next = new Set(prev)
                if (checked) next.add(c)
                else next.delete(c)
                return next
              })
            }
            onConfirm={handleRefresh}
            confirming={refreshing}
            dateRange={{
              from: refreshFrom,
              to: refreshTo,
              onFromChange: setRefreshFrom,
              onToChange: setRefreshTo,
            }}
          />
        )}
      </div>

      {customRangeIncomplete && (
        <p className="text-muted-foreground text-sm">
          Select both From and To to use a custom range.
        </p>
      )}

      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}

      {loading && !error && !customRangeIncomplete && (
        <div className="flex items-center gap-2">
          <Spinner />
          <span className="text-muted-foreground text-sm">Loading…</span>
        </div>
      )}

      {!loading && !error && !hasAnyData && !customRangeIncomplete && (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BarChart3 />
            </EmptyMedia>
            <EmptyTitle>Nothing to show yet</EmptyTitle>
            <EmptyDescription>
              No transactions match this period. Record a transaction or widen
              the date range.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button asChild>
              <Link to="/transactions">Go to transactions</Link>
            </Button>
          </EmptyContent>
        </Empty>
      )}

      {!loading && !error && hasAnyData && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          {trends && (
            <Card>
              <CardHeader>
                <CardTitle>Trends</CardTitle>
              </CardHeader>
              <CardContent className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <p className="text-muted-foreground">
                    {periodPhrase(granularity, 'current')} (
                    {trends.current.from} – {trends.current.to})
                  </p>
                  <p className="text-lg font-medium tabular-nums">
                    {trends.current.net} {trends.currency}
                  </p>
                  <p className="text-muted-foreground text-xs">
                    Inflow {changeLabel(trends.inflow_change_pct)} · Outflow{' '}
                    {changeLabel(trends.outflow_change_pct)} vs.{' '}
                    {previousPeriodPhrase(granularity)}
                  </p>
                </div>
                <div>
                  <p className="text-muted-foreground">
                    {periodPhrase(granularity, 'previous')} (
                    {trends.previous.from} – {trends.previous.to})
                  </p>
                  <p className="text-lg font-medium tabular-nums">
                    {trends.previous.net} {trends.currency}
                  </p>
                </div>
              </CardContent>
            </Card>
          )}

          {savingsRate && (
            <Card>
              <CardHeader>
                <CardTitle>Savings rate</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-3xl font-semibold tabular-nums">
                  {savingsRate.rate === null || savingsRate.rate === undefined
                    ? 'n/a'
                    : `${(savingsRate.rate * 100).toFixed(1)}%`}
                </p>
                <p className="text-muted-foreground text-xs">
                  Income {savingsRate.income} {savingsRate.currency} · Outflow{' '}
                  {savingsRate.outflow} {savingsRate.currency} · Net{' '}
                  {savingsRate.net} {savingsRate.currency}
                </p>
              </CardContent>
            </Card>
          )}

          {categoryData.length > 0 && (
            <Card className="lg:col-span-2">
              <CardHeader>
                <CardTitle>Spending &amp; income by category</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-6 sm:flex-row">
                {spendingSlices.length > 0 && (
                  <div className="flex-1">
                    <CategoryDonut
                      title="Spending"
                      icon={TrendingDown}
                      slices={spendingSlices}
                    />
                    <CategoryTable rows={spendingRows} amountLabel="Spending" />
                  </div>
                )}
                {incomeSlices.length > 0 && (
                  <div className="flex-1">
                    <CategoryDonut
                      title="Income"
                      icon={TrendingUp}
                      slices={incomeSlices}
                    />
                    <CategoryTable rows={incomeRows} amountLabel="Income" />
                  </div>
                )}
              </CardContent>
            </Card>
          )}

          {cashFlowData.length > 0 && (
            <Card className="lg:col-span-2">
              <CardHeader>
                <CardTitle>Cash flow</CardTitle>
              </CardHeader>
              <CardContent>
                <ChartContainer
                  config={cashFlowChartConfig}
                  className="aspect-auto h-72 w-full"
                >
                  <BarChart data={cashFlowData}>
                    <CartesianGrid vertical={false} />
                    <XAxis dataKey="label" tickLine={false} axisLine={false} />
                    <YAxis tickLine={false} axisLine={false} />
                    <ChartTooltip content={<ChartTooltipContent />} />
                    <Bar
                      dataKey="inflow"
                      fill="var(--color-inflow)"
                      radius={4}
                    />
                    <Bar
                      dataKey="outflow"
                      fill="var(--color-outflow)"
                      radius={4}
                    />
                    <ChartLegend content={<ChartLegendContent />} />
                  </BarChart>
                </ChartContainer>
              </CardContent>
            </Card>
          )}

          {topTransactions && topTransactions.rows.length > 0 && (
            <Card className="lg:col-span-2">
              <CardHeader>
                <CardTitle>Top transactions</CardTitle>
              </CardHeader>
              <CardContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Description</TableHead>
                      <TableHead>Date</TableHead>
                      <TableHead>Category</TableHead>
                      <TableHead className="text-right">Amount</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {topTransactions.rows.map((row) => (
                      <TableRow key={row.transaction_id}>
                        <TableCell>{row.description}</TableCell>
                        <TableCell>{row.date}</TableCell>
                        <TableCell>{row.category}</TableCell>
                        <TableCell className="text-right tabular-nums">
                          {row.amount} {topTransactions.currency}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}

          {averageSize && (
            <Card>
              <CardHeader>
                <CardTitle>Average transaction size</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-3xl font-semibold tabular-nums">
                  {averageSize.overall.average} {averageSize.currency}
                </p>
                <p className="text-muted-foreground text-xs">
                  Overall, across {averageSize.overall.count} transaction
                  {averageSize.overall.count === 1 ? '' : 's'}
                </p>
                {averageSize.by_category.length > 0 && (
                  <Table className="mt-4">
                    <TableHeader>
                      <TableRow>
                        <TableHead>Category</TableHead>
                        <TableHead className="text-right">Count</TableHead>
                        <TableHead className="text-right">Average</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {averageSize.by_category.map((row) => (
                        <TableRow key={row.category}>
                          <TableCell>{row.category}</TableCell>
                          <TableCell className="text-right tabular-nums">
                            {row.count}
                          </TableCell>
                          <TableCell className="text-right tabular-nums">
                            {row.average} {averageSize.currency}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </CardContent>
            </Card>
          )}

          {categoryTrends && categoryTrends.rows.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>Category trends</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-muted-foreground mb-2 text-xs">
                  {categoryTrends.previous_from} – {categoryTrends.previous_to}{' '}
                  vs. {categoryTrends.current_from} –{' '}
                  {categoryTrends.current_to}
                </p>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Category</TableHead>
                      <TableHead className="text-right">Spending</TableHead>
                      <TableHead className="text-right">Change</TableHead>
                      <TableHead className="text-right">Income</TableHead>
                      <TableHead className="text-right">Change</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {categoryTrends.rows.map((row) => (
                      <TableRow key={row.category}>
                        <TableCell>{row.category}</TableCell>
                        <TableCell className="text-right tabular-nums">
                          {row.current.spending} {categoryTrends.currency}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {changeLabel(row.spending_change_pct)}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {row.current.income} {categoryTrends.currency}
                        </TableCell>
                        <TableCell className="text-right tabular-nums">
                          {changeLabel(row.income_change_pct)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
          )}

          {[
            breakdown,
            cashFlow,
            trends,
            savingsRate,
            topTransactions,
            averageSize,
            categoryTrends,
          ].some((r) => (r?.unconverted?.length ?? 0) > 0) && (
            <Card className="lg:col-span-2">
              <CardHeader>
                <CardTitle>Not converted</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-1">
                {[
                  breakdown,
                  cashFlow,
                  trends,
                  savingsRate,
                  topTransactions,
                  averageSize,
                  categoryTrends,
                ]
                  .flatMap((r) => r?.unconverted ?? [])
                  .map((u, i) => (
                    <div
                      key={`${u.transaction_id}-${i}`}
                      className="flex gap-2"
                    >
                      <span className="tabular-nums">
                        {u.amount} {u.currency}
                      </span>
                      <UnconvertedNote reason={u.reason} />
                    </div>
                  ))}
              </CardContent>
            </Card>
          )}
        </div>
      )}
    </div>
  )
}
