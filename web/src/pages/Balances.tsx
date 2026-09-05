// The real balances screen (issue #63), replacing routes.tsx's
// placeholder. Docs/ux-principles.md §1's "what do I have available?"
// question, answered directly: every account's balance as of today,
// exactly as the REST API computed it. This screen must never sum across
// accounts or currencies, or reformat amount through anything that
// parses it as a number — docs/architecture.md §3 reserves that decision
// for the application layer, and multi-currency conversion doesn't even
// exist until M3.
import { useEffect, useState } from 'react'

import { Card } from '@/components/ui/card'
import { Empty, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { ApiError, getBalances, type Balances } from '@/lib/api'

export function BalancesPage() {
  const [balances, setBalances] = useState<Balances | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    getBalances()
      .then((result) => {
        if (!cancelled) setBalances(result)
      })
      .catch((err: unknown) => {
        if (cancelled) return
        setError(
          err instanceof ApiError
            ? err.message
            : 'Couldn’t reach the server. Try again.',
        )
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (error) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 p-12 text-center">
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      </div>
    )
  }

  if (balances === null) {
    return (
      <div className="flex flex-1 items-center justify-center gap-2 p-12">
        <Spinner />
        <span className="text-muted-foreground text-sm">Loading…</span>
      </div>
    )
  }

  if (balances.balances.length === 0) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 p-12 text-center">
        <h1 className="text-2xl font-semibold tracking-tight">Balances</h1>
        <Empty>
          <EmptyTitle>No accounts yet.</EmptyTitle>
        </Empty>
      </div>
    )
  }

  return (
    <div className="flex flex-1 flex-col gap-6 p-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Balances</h1>
        <p className="text-muted-foreground text-sm">As of {balances.as_of}</p>
      </div>
      <Card className="max-w-md [--card-spacing:0]">
        <ul className="divide-border divide-y">
          {balances.balances.map((b) => (
            <li
              key={b.account_id}
              className="flex items-center justify-between px-4 py-3"
            >
              <span className="text-sm font-medium">{b.account}</span>
              <span className="text-sm tabular-nums">
                {b.amount} {b.currency}
              </span>
            </li>
          ))}
        </ul>
      </Card>
    </div>
  )
}
