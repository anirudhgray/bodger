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
import { BarChart3 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { toast } from 'sonner'

import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  ChartContainer,
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
  ApiError,
  fetchFxRates,
  getCashFlow,
  getCategoryBreakdown,
  getReportingCurrency,
  getSavingsRate,
  getTrends,
  listAccounts,
  type CashFlow,
  type CategoryBreakdown,
  type SavingsRate,
  type Trends,
} from '@/lib/api'

function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : 'Couldn’t reach the server. Try again.'
}

const categoryChartConfig: ChartConfig = {
  spending: { label: 'Spending', color: 'var(--chart-2)' },
  income: { label: 'Income', color: 'var(--chart-1)' },
}

const cashFlowChartConfig: ChartConfig = {
  inflow: { label: 'Inflow', color: 'var(--chart-1)' },
  outflow: { label: 'Outflow', color: 'var(--chart-2)' },
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

export function AnalyticsPage() {
  const [currency, setCurrency] = useState('')
  const [currencies, setCurrencies] = useState<string[]>([])
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')

  const [breakdown, setBreakdown] = useState<CategoryBreakdown | null>(null)
  const [cashFlow, setCashFlow] = useState<CashFlow | null>(null)
  const [trends, setTrends] = useState<Trends | null>(null)
  const [savingsRate, setSavingsRate] = useState<SavingsRate | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [refreshOpen, setRefreshOpen] = useState(false)
  const [refreshCurrencies, setRefreshCurrencies] = useState<Set<string>>(
    new Set(),
  )
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

  const load = useCallback(() => {
    if (!currency) return
    setLoading(true)
    setError(null)
    const filter = { from: from || undefined, to: to || undefined }
    const options = { currency, policy: 'transaction_date' as const }
    Promise.all([
      getCategoryBreakdown(filter, options),
      getCashFlow(filter, options),
      getTrends(options),
      getSavingsRate(filter, options),
    ])
      .then(([b, cf, t, sr]) => {
        setBreakdown(b)
        setCashFlow(cf)
        setTrends(t)
        setSavingsRate(sr)
      })
      .catch((err: unknown) => setError(errorMessage(err)))
      .finally(() => setLoading(false))
  }, [currency, from, to])

  useEffect(() => {
    load()
  }, [load])

  // Every currency any of the four results flagged as unconverted — the
  // popover's candidate list. Deduplicated across all four since a
  // missing rate for, say, JPY affects every metric that touched a JPY
  // posting the same way.
  const unconvertedCurrencies = useMemo(() => {
    const set = new Set<string>()
    for (const result of [breakdown, cashFlow, trends, savingsRate]) {
      for (const u of result?.unconverted ?? []) set.add(u.currency)
    }
    return Array.from(set).sort()
  }, [breakdown, cashFlow, trends, savingsRate])

  function openRefresh(open: boolean) {
    setRefreshOpen(open)
    if (open) setRefreshCurrencies(new Set(unconvertedCurrencies))
  }

  async function handleRefresh() {
    setRefreshing(true)
    try {
      const range = from && to ? { from, to } : undefined
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

  const categoryData = useMemo(
    () =>
      (breakdown?.rows ?? []).map((row) => ({
        category: row.category,
        spending: Number(row.spending),
        income: Number(row.income),
      })),
    [breakdown],
  )

  const cashFlowData = useMemo(
    () =>
      (cashFlow?.points ?? []).map((point) => ({
        month: point.month,
        inflow: Number(point.inflow),
        outflow: Number(point.outflow),
      })),
    [cashFlow],
  )

  const hasAnyData =
    categoryData.length > 0 ||
    cashFlowData.length > 0 ||
    (trends !== null &&
      (trends.current.inflow !== '0' || trends.current.outflow !== '0'))

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
                  reporting currency itself even if no account happens to
                  use it yet — mirrors Balances' own "Show in" selector
                  derivation. */}
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
            dateRange={
              from && to
                ? { from, to, onFromChange: setFrom, onToChange: setTo }
                : undefined
            }
          />
        )}
      </div>

      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}

      {loading && !error && (
        <div className="flex items-center gap-2">
          <Spinner />
          <span className="text-muted-foreground text-sm">Loading…</span>
        </div>
      )}

      {!loading && !error && !hasAnyData && (
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
                    This month ({trends.current.from} – {trends.current.to})
                  </p>
                  <p className="text-lg font-medium tabular-nums">
                    {trends.current.net} {trends.currency}
                  </p>
                  <p className="text-muted-foreground text-xs">
                    Inflow {changeLabel(trends.inflow_change_pct)} · Outflow{' '}
                    {changeLabel(trends.outflow_change_pct)} vs. last month
                  </p>
                </div>
                <div>
                  <p className="text-muted-foreground">
                    Last month ({trends.previous.from} – {trends.previous.to})
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
              <CardContent>
                <ChartContainer
                  config={categoryChartConfig}
                  className="aspect-auto h-72 w-full"
                >
                  <BarChart data={categoryData} layout="vertical">
                    <CartesianGrid horizontal={false} />
                    <XAxis type="number" tickLine={false} axisLine={false} />
                    <YAxis
                      dataKey="category"
                      type="category"
                      tickLine={false}
                      axisLine={false}
                      width={110}
                    />
                    <ChartTooltip content={<ChartTooltipContent />} />
                    <Bar
                      dataKey="spending"
                      fill="var(--color-spending)"
                      radius={4}
                    />
                    <Bar
                      dataKey="income"
                      fill="var(--color-income)"
                      radius={4}
                    />
                  </BarChart>
                </ChartContainer>
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
                    <XAxis dataKey="month" tickLine={false} axisLine={false} />
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
                  </BarChart>
                </ChartContainer>
              </CardContent>
            </Card>
          )}

          {[breakdown, cashFlow, trends, savingsRate].some(
            (r) => (r?.unconverted?.length ?? 0) > 0,
          ) && (
            <Card className="lg:col-span-2">
              <CardHeader>
                <CardTitle>Not converted</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-1">
                {[breakdown, cashFlow, trends, savingsRate]
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
