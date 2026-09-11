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
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Collapsible } from '@/components/ui/collapsible'
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
  getReportingCurrency,
  type Balances,
} from '@/lib/api'

function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : 'Couldn’t reach the server. Try again.'
}

// The sentinel Select value for "no conversion, show each account in its
// own currency" — the screen's original (M2) behaviour. Radix Select
// doesn't allow an empty-string item value, so this has to be a real,
// non-conflicting token rather than ''.
const NO_CONVERSION = 'original'

export function BalancesPage() {
  const [initial, setInitial] = useState<Balances | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [reportingCurrency, setReportingCurrency] = useState<string | null>(
    null,
  )
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
