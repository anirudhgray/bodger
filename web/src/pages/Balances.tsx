// The real balances screen (issue #63), replacing routes.tsx's
// placeholder. Docs/ux-principles.md §1's "what do I have available?"
// question, answered directly: every account's balance as of today,
// exactly as the REST API computed it. This screen must never sum across
// accounts or currencies, or reformat amount through anything that
// parses it as a number — docs/architecture.md §3 reserves that decision
// for the application layer, and multi-currency conversion doesn't even
// exist until M3.
import { Wallet } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'

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
import { Spinner } from '@/components/ui/spinner'
import { ApiError, getBalances, type Balances } from '@/lib/api'

export function BalancesPage() {
  const [balances, setBalances] = useState<Balances | null>(null)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

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
      <div className="flex flex-1 flex-col gap-6 p-6">
        <h1 className="text-2xl font-semibold tracking-tight">Balances</h1>
        <div className="flex items-center gap-2">
          <Spinner />
          <span className="text-muted-foreground text-sm">Loading…</span>
        </div>
      </div>
    )
  }

  if (balances.balances.length === 0) {
    return (
      <div className="flex flex-1 flex-col gap-6 p-6">
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

  return (
    <div className="flex flex-1 flex-col gap-6 p-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">Balances</h1>
        <p className="text-muted-foreground text-sm">As of {balances.as_of}</p>
      </div>
      <Card className="max-w-md [--card-spacing:0]">
        <ul className="divide-border divide-y">
          {balances.balances.map((b) => (
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
          ))}
        </ul>
      </Card>
    </div>
  )
}
