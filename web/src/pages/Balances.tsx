// The real balances screen (issue #63), replacing routes.tsx's
// placeholder. Docs/ux-principles.md §1's "what do I have available?"
// question, answered directly: every account's balance as of today,
// exactly as the REST API computed it. This screen must never sum across
// accounts or currencies, or reformat amount through anything that
// parses it as a number — docs/architecture.md §3 reserves that decision
// for the application layer.
//
// Issue #139 layers currency conversion on top, but only once a second
// currency is actually in play (docs/ux-principles.md §4 — a
// single-currency user must never meet any of this): the currency
// selector, the rate-provenance detail row, and the refresh popover only
// render when the account list itself spans more than one currency.
// See docs/design-system.md's "Balances: rate-provenance detail row"
// section for the pattern this establishes.
import { Wallet } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import { CartesianGrid, Line, LineChart, XAxis, YAxis } from 'recharts'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@/components/ui/chart'
import { Collapsible } from '@/components/ui/collapsible'
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
import {
  ApiError,
  fetchFxRates,
  getBalances,
  getBalanceTotals,
  getNetWorthOverTime,
  getReportingCurrency,
  type Balances,
  type BalanceTotals,
  type Granularity,
  type NetWorthOverTime,
} from '@/lib/api'

function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : 'Couldn’t reach the server. Try again.'
}

// formatAccountKind turns the wire account kind ("credit_card") into a
// display label ("Credit card") — display-only, never fed back into a
// request.
function formatAccountKind(kind: string): string {
  const spaced = kind.replace(/_/g, ' ')
  return spaced.charAt(0).toUpperCase() + spaced.slice(1)
}

// The sentinel Select value for "no conversion, show each account in its
// own currency" — the screen's original (M2) behaviour. Radix Select
// doesn't allow an empty-string item value, so this has to be a real,
// non-conflicting token rather than ''.
const NO_CONVERSION = 'original'

// netWorthChartConfig is the net-worth-over-time line chart's single
// series — one line, no legend needed (unlike Analytics' two-series cash
// flow bars).
const netWorthChartConfig: ChartConfig = {
  amount: { label: 'Net worth', color: 'var(--chart-1)' },
}

export function BalancesPage() {
  const [initial, setInitial] = useState<Balances | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [reportingCurrency, setReportingCurrency] = useState<string | null>(
    null,
  )
  const [totals, setTotals] = useState<BalanceTotals | null>(null)
  const [totalsError, setTotalsError] = useState<string | null>(null)
  const [targetCurrency, setTargetCurrency] = useState(NO_CONVERSION)
  const [converted, setConverted] = useState<Balances | null>(null)
  const [converting, setConverting] = useState(false)
  const [convertError, setConvertError] = useState<string | null>(null)
  const [expandedAccountId, setExpandedAccountId] = useState<string | null>(
    null,
  )
  const [refreshOpen, setRefreshOpen] = useState(false)
  const [refreshPairs, setRefreshPairs] = useState<Set<string>>(new Set())
  const [refreshing, setRefreshing] = useState(false)
  const navigate = useNavigate()

  // Net worth over time (issue #196) — its own time-series chart section,
  // distinct from the point-in-time totals cards above: a date range plus
  // granularity, left blank by default rather than a client-guessed
  // "today minus N months" window (the same "no client-side maths/`now`"
  // rule Analytics.tsx's own filter chrome already follows).
  const [netWorthFrom, setNetWorthFrom] = useState('')
  const [netWorthTo, setNetWorthTo] = useState('')
  const [netWorthGranularity, setNetWorthGranularity] =
    useState<Granularity>('month')
  const [netWorth, setNetWorth] = useState<NetWorthOverTime | null>(null)
  const [netWorthLoading, setNetWorthLoading] = useState(false)
  const [netWorthError, setNetWorthError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    getBalances()
      .then((result) => {
        if (!cancelled) setInitial(result)
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errorMessage(err))
      })
    // Best-effort only: this just labels which currency option is the
    // user's reporting currency in the selector below. A failure here
    // doesn't block the balances themselves from rendering.
    getReportingCurrency()
      .then((result) => {
        if (!cancelled) setReportingCurrency(result.effectiveCurrency)
      })
      .catch(() => {})
    // The totals overview (issue #195) — its own section, independent of
    // the per-account "Show in" selector below: it always renders in the
    // server-resolved reporting currency, the number that answers
    // "what's my overall position", not whatever one-off currency the
    // account list happens to be displayed in right now.
    getBalanceTotals('current')
      .then((result) => {
        if (!cancelled) setTotals(result)
      })
      .catch((err: unknown) => {
        if (!cancelled) setTotalsError(errorMessage(err))
      })
    return () => {
      cancelled = true
    }
  }, [])

  // Every currency actually in use across the account list — the single
  // source of truth for whether multi-currency UI may appear at all.
  const currencies = useMemo(() => {
    if (!initial) return []
    return Array.from(new Set(initial.balances.map((b) => b.currency))).sort()
  }, [initial])

  const isMultiCurrency = currencies.length > 1

  const candidatePairs = useMemo(
    () => currencies.filter((c) => c !== targetCurrency),
    [currencies, targetCurrency],
  )

  // loadNetWorth fetches whenever both bounds are set — left unset, this
  // section just shows its own "pick a range" hint rather than fetching
  // (there's no sensible default range for a time series).
  const loadNetWorth = useCallback(() => {
    if (!netWorthFrom || !netWorthTo) {
      setNetWorth(null)
      setNetWorthError(null)
      return
    }
    setNetWorthLoading(true)
    setNetWorthError(null)
    getNetWorthOverTime(
      { from: netWorthFrom, to: netWorthTo },
      'current',
      undefined,
      netWorthGranularity,
    )
      .then((result) => setNetWorth(result))
      .catch((err: unknown) => setNetWorthError(errorMessage(err)))
      .finally(() => setNetWorthLoading(false))
  }, [netWorthFrom, netWorthTo, netWorthGranularity])

  useEffect(() => {
    // loadNetWorth fetches from the balances API (an external system)
    // whenever the range/granularity change — not a value derivable
    // during render.
    // oxlint-disable-next-line react/set-state-in-effect
    loadNetWorth()
  }, [loadNetWorth])

  // amount is parsed to a number only for the chart's y-axis position —
  // never combined with anything else (ADR-0009's "no client-side maths").
  const netWorthChartData = useMemo(
    () =>
      (netWorth?.points ?? []).map((p) => ({
        date: p.date,
        amount: Number(p.amount),
      })),
    [netWorth],
  )

  function loadConverted(currency: string) {
    setConverting(true)
    setConvertError(null)
    getBalances({ currency, policy: 'current' })
      .then((result) => {
        setConverted(result)
      })
      .catch((err: unknown) => {
        setConvertError(errorMessage(err))
      })
      .finally(() => setConverting(false))
  }

  function handleCurrencyChange(value: string) {
    setTargetCurrency(value)
    setExpandedAccountId(null)
    if (value === NO_CONVERSION) {
      setConverted(null)
      setConvertError(null)
      return
    }
    loadConverted(value)
  }

  function openRefresh(open: boolean) {
    setRefreshOpen(open)
    if (!open) return
    // Default the popover's checkboxes to whichever pairs actually need
    // it (stale or missing entirely) — every candidate stays selectable,
    // this just saves the common case a click.
    const unconvertedNames = new Set(
      (displayed?.unconverted ?? []).map((u) => u.account),
    )
    const needsRefresh = new Set<string>()
    for (const b of displayed?.balances ?? []) {
      if (b.currency === targetCurrency) continue
      if (b.converted?.stale || unconvertedNames.has(b.account)) {
        needsRefresh.add(b.currency)
      }
    }
    setRefreshPairs(needsRefresh.size > 0 ? needsRefresh : new Set())
  }

  async function handleRefresh() {
    setRefreshing(true)
    try {
      // quote must be targetCurrency (the "Show in" selection), not the
      // omitted default (the actor's reporting currency) — otherwise a
      // refresh while viewing a non-reporting currency fetches pairs
      // quoted against the wrong currency, which can even self-skip
      // silently when the resolved reporting currency happens to equal
      // one of the requested pairs (issue #172).
      await fetchFxRates(Array.from(refreshPairs), undefined, targetCurrency)
      toast.success('Rates refreshed.')
      setRefreshOpen(false)
      loadConverted(targetCurrency)
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setRefreshing(false)
    }
  }

  if (error) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 p-12 text-center">
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      </div>
    )
  }

  if (initial === null) {
    return (
      <div className="flex flex-1 flex-col gap-6 p-4 sm:p-6">
        <h1 className="text-2xl font-semibold tracking-tight">Balances</h1>
        <div className="flex items-center gap-2">
          <Spinner />
          <span className="text-muted-foreground text-sm">Loading…</span>
        </div>
      </div>
    )
  }

  if (initial.balances.length === 0) {
    return (
      <div className="flex flex-1 flex-col gap-6 p-4 sm:p-6">
        <h1 className="text-2xl font-semibold tracking-tight">Balances</h1>
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Wallet />
            </EmptyMedia>
            <EmptyTitle>No accounts yet</EmptyTitle>
            <EmptyDescription>
              Add an account to start tracking balances.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button asChild>
              <Link to="/settings">Add an account</Link>
            </Button>
          </EmptyContent>
        </Empty>
      </div>
    )
  }

  // Fall back to the uncoverted list while a conversion is in flight (or
  // failed) rather than blanking the whole table — amounts just show in
  // their own currency until the converted view arrives.
  const displayed =
    targetCurrency === NO_CONVERSION ? initial : (converted ?? initial)

  return (
    <div className="flex flex-1 flex-col gap-6 p-4 sm:p-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Balances</h1>
        <p className="text-muted-foreground text-sm">As of {displayed.as_of}</p>
      </div>

      {totalsError && (
        <p role="alert" className="text-destructive text-sm">
          {totalsError}
        </p>
      )}

      {totals && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Card>
            <CardHeader>
              <CardTitle>Overall</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-3xl font-semibold tabular-nums">
                {totals.overall} {totals.currency}
              </p>
            </CardContent>
          </Card>

          {totals.by_category.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>By category</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-1.5 text-sm">
                {totals.by_category.map((c) => (
                  <div
                    key={c.kind}
                    className="flex items-center justify-between"
                  >
                    <span className="text-muted-foreground">
                      {formatAccountKind(c.kind)}
                    </span>
                    <span className="tabular-nums">
                      {c.amount} {totals.currency}
                    </span>
                  </div>
                ))}
              </CardContent>
            </Card>
          )}

          {isMultiCurrency && totals.by_currency.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle>By currency</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-1.5 text-sm">
                {totals.by_currency.map((c) => (
                  <div
                    key={c.currency}
                    className="flex items-center justify-between"
                  >
                    <span className="text-muted-foreground">{c.currency}</span>
                    <span className="tabular-nums">
                      {c.amount} {c.currency}
                    </span>
                  </div>
                ))}
              </CardContent>
            </Card>
          )}

          {totals.unconverted && totals.unconverted.length > 0 && (
            <Card className="sm:col-span-2 lg:col-span-3">
              <CardHeader>
                <CardTitle>Not included in overall/by category</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-1">
                {totals.unconverted.map((u) => (
                  <div key={u.account} className="flex items-center gap-2">
                    <span className="text-sm font-medium">{u.account}</span>
                    <UnconvertedNote reason={u.reason} />
                  </div>
                ))}
              </CardContent>
            </Card>
          )}
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Net worth over time</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-wrap items-end gap-3">
            <div className="flex flex-col gap-1">
              <Label htmlFor="networth-from">From</Label>
              <DatePicker
                id="networth-from"
                value={netWorthFrom}
                onChange={setNetWorthFrom}
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor="networth-to">To</Label>
              <DatePicker
                id="networth-to"
                value={netWorthTo}
                onChange={setNetWorthTo}
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label
                htmlFor="networth-granularity"
                className="text-muted-foreground text-sm font-normal"
              >
                Granularity
              </Label>
              <Select
                value={netWorthGranularity}
                onValueChange={(v) => setNetWorthGranularity(v as Granularity)}
              >
                <SelectTrigger id="networth-granularity" className="w-28">
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
          </div>

          {!netWorthFrom || !netWorthTo ? (
            <p className="text-muted-foreground text-sm">
              Select both From and To to plot net worth over time.
            </p>
          ) : netWorthError ? (
            <p role="alert" className="text-destructive text-sm">
              {netWorthError}
            </p>
          ) : netWorthLoading ? (
            <div className="flex items-center gap-2">
              <Spinner />
              <span className="text-muted-foreground text-sm">Loading…</span>
            </div>
          ) : netWorthChartData.length > 0 ? (
            <>
              <ChartContainer
                config={netWorthChartConfig}
                className="aspect-auto h-64 w-full"
              >
                <LineChart data={netWorthChartData}>
                  <CartesianGrid vertical={false} />
                  <XAxis dataKey="date" tickLine={false} axisLine={false} />
                  <YAxis tickLine={false} axisLine={false} />
                  <ChartTooltip content={<ChartTooltipContent />} />
                  <Line
                    dataKey="amount"
                    stroke="var(--color-amount)"
                    dot={false}
                    strokeWidth={2}
                  />
                </LineChart>
              </ChartContainer>
              {netWorth && (netWorth.unconverted?.length ?? 0) > 0 && (
                <div className="flex flex-col gap-1">
                  {(netWorth.unconverted ?? []).map((u) => (
                    <div key={u.account} className="flex items-center gap-2">
                      <span className="text-sm font-medium">{u.account}</span>
                      <UnconvertedNote reason={u.reason} />
                    </div>
                  ))}
                </div>
              )}
            </>
          ) : (
            <p className="text-muted-foreground text-sm">
              No points in this range.
            </p>
          )}
        </CardContent>
      </Card>

      {isMultiCurrency && (
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex items-center gap-2">
            <Label
              htmlFor="balances-currency"
              className="text-muted-foreground text-sm font-normal"
            >
              Show in
            </Label>
            <Select value={targetCurrency} onValueChange={handleCurrencyChange}>
              <SelectTrigger id="balances-currency" className="w-48">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NO_CONVERSION}>
                  Original currencies
                </SelectItem>
                {currencies.map((c) => (
                  <SelectItem key={c} value={c}>
                    {c}
                    {c === reportingCurrency ? ' (reporting currency)' : ''}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {converting && (
            <span className="text-muted-foreground flex items-center gap-1.5 text-xs">
              <Spinner className="size-3.5" />
              Converting…
            </span>
          )}

          {convertError && (
            <span role="alert" className="text-destructive text-xs">
              {convertError}
            </span>
          )}

          {targetCurrency !== NO_CONVERSION && candidatePairs.length > 0 && (
            <RateFetchPopover
              open={refreshOpen}
              onOpenChange={openRefresh}
              triggerLabel="Refresh rates"
              title="Refresh rates"
              description="Fetch today's rate for the currencies you pick."
              candidates={candidatePairs}
              selected={refreshPairs}
              onToggle={(currency, checked) => {
                setRefreshPairs((prev) => {
                  const next = new Set(prev)
                  if (checked) next.add(currency)
                  else next.delete(currency)
                  return next
                })
              }}
              onConfirm={handleRefresh}
              confirming={refreshing}
              confirmLabel="Refresh"
            />
          )}
        </div>
      )}

      <Card className="max-w-md [--card-spacing:0]">
        <ul className="divide-border divide-y">
          {displayed.balances.map((b) => {
            if (!isMultiCurrency) {
              return (
                <li key={b.account_id}>
                  <button
                    type="button"
                    onClick={() =>
                      navigate('/transactions', {
                        state: { filter: { account: b.account_id } },
                      })
                    }
                    className="hover:bg-accent/50 flex w-full items-center justify-between px-4 py-3 text-left transition-colors"
                  >
                    <span className="text-sm font-medium">{b.account}</span>
                    <span className="text-sm tabular-nums">
                      {b.amount} {b.currency}
                    </span>
                  </button>
                </li>
              )
            }

            const isTarget = b.currency === targetCurrency
            const isUnconverted =
              targetCurrency !== NO_CONVERSION && !isTarget && !b.converted
            const unconvertedReason = isUnconverted
              ? displayed.unconverted?.find((u) => u.account === b.account)
                  ?.reason
              : undefined
            const expanded = expandedAccountId === b.account_id

            return (
              <li key={b.account_id}>
                <Collapsible
                  open={expanded}
                  onOpenChange={(open) =>
                    setExpandedAccountId(open ? b.account_id : null)
                  }
                >
                  <div className="hover:bg-accent/50 flex w-full flex-wrap items-center justify-between gap-x-3 gap-y-1 px-4 py-3 transition-colors">
                    <button
                      type="button"
                      onClick={() =>
                        navigate('/transactions', {
                          state: { filter: { account: b.account_id } },
                        })
                      }
                      className="min-w-0 flex-1 truncate text-left text-sm font-medium"
                    >
                      {b.account}
                    </button>
                    <div className="flex items-center gap-2">
                      {b.converted && (
                        <RateAmountTrigger stale={b.converted.stale}>
                          ≈ {b.converted.amount} {b.converted.currency}
                        </RateAmountTrigger>
                      )}
                      {isUnconverted && (
                        <UnconvertedNote reason={unconvertedReason} />
                      )}
                      <span className="text-sm tabular-nums">
                        {b.amount} {b.currency}
                      </span>
                    </div>
                  </div>
                  {b.converted && (
                    <RateProvenanceDetail>
                      1 {b.currency} = {b.converted.rate} {b.converted.currency}
                      {' · as of '}
                      {b.converted.rate_date}
                      {' · '}
                      {b.converted.rate_source}
                      {b.converted.stale && ' · stale'}
                    </RateProvenanceDetail>
                  )}
                </Collapsible>
              </li>
            )
          })}
        </ul>
      </Card>
    </div>
  )
}
