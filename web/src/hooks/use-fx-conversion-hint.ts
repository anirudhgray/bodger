import { useCallback, useEffect, useRef, useState } from 'react'

import { ApiError, fetchFxRates, getFxRate, type FxRate } from '@/lib/api'

export type FxHintState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ready'; rate: FxRate }
  | { status: 'unavailable' }
  | { status: 'error'; message: string }

export type FxHint = {
  state: FxHintState
  refreshing: boolean
  // Only true when a refresh can actually target this exact pair (issue
  // #138's narrowly-scoped refresh): POST /api/v1/fx/rates/fetch's
  // "pairs" always fetches <base>/<reporting-currency> — see this file's
  // own doc comment for why "to" other than the reporting currency is
  // never fetchable, so the caller shouldn't offer a refresh action it
  // can't honour.
  canRefresh: boolean
  refresh: () => void
}

// useFxConversionHint drives every non-persisted "≈ N <currency> as of
// <date>" read in TransactionDialog (issue #138) — a read-only display,
// never written to the transaction being entered; whatever it resolves is
// only ever a hint, not a fact recorded anywhere until the transaction is
// actually submitted (which happens through the normal amount/account
// fields, not through this hook).
//
// This only ever asks for a rate quoted against the reporting currency:
// internal/adapters/sqlite/fx_rate_repo.go's Lookup does an exact
// base/quote match against stored rows, and every stored row's quote is
// the actor's reporting currency (internal/app/fetch_fx_rates.go's
// resolveFetchPairs) — so `to` here should always be the reporting
// currency, never an arbitrary second account's own currency (there is
// no cross-rate/triangulation support yet to resolve that).
//
// `date` is the *entered* transaction date, not today's — an empty date
// means "today" the same way the rest of the dialog treats it, resolved
// server-side via policy=current (ADR-0005's normalise-once) rather than
// this hook computing "today" itself.
export function useFxConversionHint({
  from,
  to,
  amount,
  date,
}: {
  from: string
  to: string
  amount: string
  date: string
}): FxHint {
  const trimmedAmount = amount.trim()
  const enabled =
    from !== '' && to !== '' && from !== to && trimmedAmount !== ''

  const [state, setState] = useState<FxHintState>({ status: 'idle' })
  const [refreshing, setRefreshing] = useState(false)
  // Guards against an in-flight request for a since-superseded
  // amount/date resolving after a later one already has (e.g. typing
  // "8", "80", "800" fires three overlapping lookups) — only the most
  // recently issued request is allowed to write state.
  const requestIdRef = useRef(0)

  const load = useCallback(() => {
    const requestId = ++requestIdRef.current
    if (!enabled) {
      setState({ status: 'idle' })
      return
    }
    setState({ status: 'loading' })
    getFxRate({
      from,
      to,
      amount: trimmedAmount,
      ...(date
        ? { policy: 'transaction_date', transactionDate: date }
        : { policy: 'current' }),
    })
      .then((rate) => {
        if (requestId !== requestIdRef.current) return
        setState({ status: 'ready', rate })
      })
      .catch((err: unknown) => {
        if (requestId !== requestIdRef.current) return
        if (err instanceof ApiError && err.code === 'not_found') {
          setState({ status: 'unavailable' })
        } else {
          setState({
            status: 'error',
            message:
              err instanceof ApiError
                ? err.message
                : 'Couldn’t check the exchange rate.',
          })
        }
      })
  }, [enabled, from, to, trimmedAmount, date])

  // Debounced: this fires on every keystroke in the amount field via
  // `amount`/`trimmedAmount` changing, and there is no reason to hit the
  // API for every intermediate value typed on the way to a final amount.
  useEffect(() => {
    const timer = setTimeout(load, 400)
    return () => clearTimeout(timer)
  }, [load])

  const refresh = useCallback(() => {
    if (!enabled || refreshing) return
    setRefreshing(true)
    fetchFxRates([from], date || undefined)
      .then(() => load())
      .catch((err: unknown) => {
        setState({
          status: 'error',
          message:
            err instanceof ApiError
              ? err.message
              : 'Couldn’t refresh the exchange rate.',
        })
      })
      .finally(() => setRefreshing(false))
  }, [enabled, refreshing, from, date, load])

  return { state, refreshing, canRefresh: enabled, refresh }
}
