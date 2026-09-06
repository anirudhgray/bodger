// A non-persisted "≈ N <currency> as of <date>" read (issue #138) — never
// written to the transaction being entered, only ever a hint alongside an
// amount typed in a currency other than the reporting currency. Shared by
// every amount field in TransactionDialog that needs one, so the
// loading/stale/unavailable/error states render identically everywhere.
import { RefreshCwIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/spinner'
import type { FxHint } from '@/hooks/use-fx-conversion-hint'

export function FxConversionHint({ hint }: { hint: FxHint }) {
  const { state, refreshing, canRefresh, refresh } = hint

  if (state.status === 'idle') return null

  return (
    <p className="text-muted-foreground flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs">
      {state.status === 'loading' && (
        <>
          <Spinner className="size-3" />
          <span>Checking the exchange rate…</span>
        </>
      )}
      {state.status === 'ready' && (
        <span>
          {/* state.rate.converted already carries its own currency code
              (internal/surface/http/fx.go's fxRateViewFrom: "%s %s" of
              amount and currency) — appending a separate currency here
              would render it twice. */}
          ≈ {state.rate.converted} as of {state.rate.rate_date}
          {state.rate.stale ? ' (stale)' : ''}
        </span>
      )}
      {state.status === 'unavailable' && (
        <span>No stored exchange rate yet.</span>
      )}
      {state.status === 'error' && (
        <span className="text-destructive">{state.message}</span>
      )}
      {canRefresh && state.status !== 'loading' && (
        <Button
          type="button"
          variant="ghost"
          size="xs"
          onClick={refresh}
          disabled={refreshing}
          className="h-auto gap-1 p-0 text-xs underline-offset-2 hover:bg-transparent hover:underline"
        >
          <RefreshCwIcon className={refreshing ? 'animate-spin' : undefined} />
          {refreshing
            ? 'Refreshing…'
            : state.status === 'unavailable'
              ? 'Fetch rate'
              : 'Refresh'}
        </Button>
      )}
    </p>
  )
}
