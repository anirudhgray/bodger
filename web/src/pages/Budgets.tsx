// The budgets screen (issue #245, M7 · Gondor): create/edit a monthly
// category budget and its lines, and see actual-vs-budget, remaining, and
// utilisation for a period, with previous/next navigation across months.
// Rollover, planned income, custom periods, and non-monthly budgets all
// stay deferred (data-model.md §13) — nothing here plans for them.
//
// Every actual/remaining/utilisation figure rendered below is exactly what
// #244's application layer computed (app.BudgetLineActuals) — this screen
// never sums or derives one itself (ADR-0009's "the web UI does no maths"
// rule, the same one Analytics.tsx and Balances.tsx are held to).
//
// Period navigation reuses GET /api/v1/budgets/{id}/history with months=1
// rather than GET .../actuals — one code path for "load a period" instead
// of two, and it's the endpoint the issue's own scope calls out for period
// navigation. The very first load omits `period` entirely so the server
// resolves "the current month" itself (normalise-once — this file never
// guesses "today"); once that response comes back, its own `from` date
// becomes the reference Previous/Next shift by a month, the same "reflect
// the server's resolved now back to the client" pattern Balances' "As of
// {as_of}" already uses, rather than this screen computing its own idea of
// "today".
import { useCallback, useEffect, useMemo, useState } from 'react'
import { PiggyBank } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { BudgetDialog } from '@/components/BudgetDialog'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Progress } from '@/components/ui/progress'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { UnconvertedNote } from '@/components/RateProvenance'
import {
  archiveBudget,
  getBudgetHistory,
  listBudgets,
  listCategories,
  ApiError,
  type Budget,
  type BudgetActuals,
  type Category,
} from '@/lib/api'

function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : 'Couldn’t reach the server. Try again.'
}

// shiftMonth moves an ISO "YYYY-MM-DD" date by `delta` calendar months,
// keeping the day fixed at 1st — every caller here only ever feeds this a
// period's own `from` date, which a monthly budget period always starts on
// the 1st (internal/app/budget_actuals.go's monthRange). Plain date-field
// arithmetic on a date the server already resolved, not a new figure
// computed from nothing (see this file's own top comment).
function shiftMonth(isoDate: string, delta: number): string {
  const [year, month] = isoDate.split('-').map(Number)
  const totalMonths = year * 12 + (month - 1) + delta
  const newYear = Math.floor(totalMonths / 12)
  const newMonth = ((totalMonths % 12) + 12) % 12
  return `${newYear}-${String(newMonth + 1).padStart(2, '0')}-01`
}

function formatMonthLabel(isoDate: string): string {
  return new Date(`${isoDate}T00:00:00`).toLocaleDateString('en-US', {
    month: 'long',
    year: 'numeric',
  })
}

// daysBetween counts whole calendar days from ISO date `a` to ISO date
// `b`, via UTC millis so a DST transition in the viewer's own local
// timezone can't shift the count by one — the dates themselves already
// came from the server pre-resolved in the actor's own configured
// timezone (BudgetActualsResult.From/To/AsOf), so this is pure calendar
// arithmetic on values already fixed, not a second timezone resolution.
function daysBetween(a: string, b: string): number {
  const toUTCDays = (iso: string) => {
    const [y, m, d] = iso.split('-').map(Number)
    return Date.UTC(y, m - 1, d)
  }
  return Math.round((toUTCDays(b) - toUTCDays(a)) / 86_400_000)
}

// MonthProgress is how far through a period's own [from, to] range
// `as_of` falls — percent (0-100) drives the marker's position, and
// elapsedDays/totalDays back its hover title (see the Progress
// component's own markerTitle doc comment for why a plain title rather
// than a Tooltip).
type MonthProgress = { percent: number; elapsedDays: number; totalDays: number }

// monthProgress returns null when as_of is outside the period's range
// entirely (a past period, already fully elapsed, or a future one nobody
// has reached yet), since the marker is only meaningful for whichever
// period is actually in progress right now. This is date arithmetic on
// three dates the server already resolved, not a financial figure this
// screen is computing itself — the same category of client-side math
// shiftMonth above already does, distinct from ADR-0009's "no summing,
// converting, or rolling up" rule for money.
function monthProgress(period: BudgetActuals): MonthProgress | null {
  const totalDays = daysBetween(period.from, period.to) + 1
  const elapsedDays = daysBetween(period.from, period.as_of) + 1
  if (elapsedDays < 1 || elapsedDays > totalDays) return null
  return { percent: (elapsedDays / totalDays) * 100, elapsedDays, totalDays }
}

function monthProgressTitle(progress: MonthProgress): string {
  return `Day ${progress.elapsedDays} of ${progress.totalDays} in this period (${Math.round(progress.percent)}%) — compare against the fill to see if spending is ahead of or behind pace.`
}

type UtilisationStatus = 'under' | 'at' | 'over'

// utilisationStatus buckets a line's actual/budgeted ratio into the three
// states the issue asks the progress indicator to clearly distinguish.
// "At" covers a narrow band approaching the limit rather than requiring
// exact equality, which decimal division rarely lands on exactly.
function utilisationStatus(utilisation: number): UtilisationStatus {
  if (utilisation > 1) return 'over'
  if (utilisation >= 0.9) return 'at'
  return 'under'
}

const UTILISATION_LABEL: Record<UtilisationStatus, string> = {
  under: 'Under budget',
  at: 'At budget',
  over: 'Over budget',
}

// Only `over` gets the destructive color — docs/design-system.md reserves
// `success` for confirmation text, not fills, so "under"/"at" stay the
// same neutral/primary bar and are told apart by their label and percentage
// instead of a second invented color.
function utilisationBarClassName(status: UtilisationStatus): string {
  return status === 'over' ? 'bg-destructive' : 'bg-primary'
}

function utilisationTextClassName(status: UtilisationStatus): string {
  return status === 'over' ? 'text-destructive' : 'text-muted-foreground'
}

// UtilisationBar is the bar + percentage + under/at/over label shared by
// the per-budget Overall row and every per-line row below it — the same
// three-piece rendering, just fed a different utilisation figure. marker
// (see monthProgress above) is the same value for the Overall bar and
// every line's bar within one period, since it depends only on the
// period's own from/to/as_of, never on a line's own numbers. Its hover
// title carries the explanation (day X of Y) rather than a persistent
// caption under every bar — the marker itself (tall, accent-colored, with
// a background ring so it stays visible over either fill color) is meant
// to draw a first look on its own.
function UtilisationBar({
  utilisation,
  marker,
}: {
  utilisation: number
  marker: MonthProgress | null
}) {
  const status = utilisationStatus(utilisation)
  return (
    <>
      <div className="flex items-center gap-2">
        <Progress
          value={Math.min(utilisation * 100, 100)}
          indicatorClassName={utilisationBarClassName(status)}
          marker={marker?.percent}
          markerTitle={marker ? monthProgressTitle(marker) : undefined}
          className="flex-1"
        />
        <span className="text-xs tabular-nums whitespace-nowrap">
          {Math.round(utilisation * 100)}%
        </span>
      </div>
      <p className={`text-xs ${utilisationTextClassName(status)}`}>
        {UTILISATION_LABEL[status]}
      </p>
    </>
  )
}

function BudgetPeriodTable({
  actuals,
  categoriesByID,
}: {
  actuals: BudgetActuals
  categoriesByID: Map<string, string>
}) {
  const marker = monthProgress(actuals)

  return (
    <>
      <div className="mb-4 flex flex-col gap-1 border-b pb-4">
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium">Overall</span>
          <span className="text-muted-foreground text-sm tabular-nums">
            {actuals.overall.actual} / {actuals.overall.budgeted}{' '}
            {actuals.currency}
          </span>
        </div>
        <UtilisationBar
          utilisation={actuals.overall.utilisation}
          marker={marker}
        />
      </div>

      {actuals.lines.length === 0 ? (
        <p className="text-muted-foreground text-sm">
          No lines yet — edit this budget to plan an amount for a category.
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Category</TableHead>
              <TableHead className="text-right">Budgeted</TableHead>
              <TableHead className="text-right">Actual</TableHead>
              <TableHead className="text-right">Remaining</TableHead>
              <TableHead className="w-40">Utilisation</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {actuals.lines.map((line) => (
              <TableRow key={line.line_id}>
                <TableCell>
                  {categoriesByID.get(line.category_id) ?? line.category_id}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {line.budgeted} {actuals.currency}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {line.actual} {actuals.currency}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {line.remaining} {actuals.currency}
                </TableCell>
                <TableCell>
                  <UtilisationBar
                    utilisation={line.utilisation}
                    marker={marker}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      {actuals.unconverted && actuals.unconverted.length > 0 && (
        <div className="mt-3 flex flex-col gap-1">
          {actuals.unconverted.map((u, i) => (
            <div key={`${u.transaction_id}-${i}`} className="flex gap-2">
              <span className="text-sm tabular-nums">
                {u.amount} {u.currency}
              </span>
              <UnconvertedNote reason={u.reason} />
            </div>
          ))}
        </div>
      )}
    </>
  )
}

export function BudgetsPage() {
  const [budgets, setBudgets] = useState<Budget[] | null>(null)
  const [categories, setCategories] = useState<Category[]>([])
  const [loadError, setLoadError] = useState<string | null>(null)

  // anchor drives the period query param (undefined lets the server
  // resolve "current month"); resolvedFrom is set from whatever the
  // server actually returned, and is what Previous/Next shift from — kept
  // separate from anchor so setting it doesn't itself retrigger the fetch
  // effect a second time for the same period.
  const [anchor, setAnchor] = useState<string | undefined>(undefined)
  const [resolvedFrom, setResolvedFrom] = useState<string | null>(null)
  const [periodsByBudget, setPeriodsByBudget] = useState<
    Record<string, BudgetActuals | null>
  >({})
  const [periodLoading, setPeriodLoading] = useState(true)
  const [periodError, setPeriodError] = useState<string | null>(null)

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingBudget, setEditingBudget] = useState<Budget | null>(null)

  const refreshBudgets = useCallback(async () => {
    try {
      const result = await listBudgets()
      setBudgets(result)
    } catch (err) {
      setLoadError(errorMessage(err))
    }
  }, [])

  useEffect(() => {
    // Fetch-on-mount: both read from the API, an external system that
    // can't be read during render.
    // oxlint-disable-next-line react/set-state-in-effect
    refreshBudgets()
    listCategories()
      .then(setCategories)
      .catch(() => {
        // Best-effort, matching TransactionsList's own categoriesByID
        // fallback — a failed load here just means lines show their raw
        // category ID instead of a name, not that the page fails.
      })
  }, [refreshBudgets])

  const activeBudgets = useMemo(
    () => (budgets ?? []).filter((b) => !b.archived),
    [budgets],
  )

  const categoriesByID = useMemo(
    () => new Map(categories.map((c) => [c.id, c.name])),
    [categories],
  )

  useEffect(() => {
    if (activeBudgets.length === 0) {
      setPeriodsByBudget({})
      setPeriodLoading(false)
      return
    }
    let cancelled = false
    setPeriodLoading(true)
    setPeriodError(null)
    Promise.all(
      activeBudgets.map((b) =>
        getBudgetHistory(b.id, anchor, 1).then(
          (h) => [b.id, h.periods[0] ?? null] as const,
        ),
      ),
    )
      .then((entries) => {
        if (cancelled) return
        setPeriodsByBudget(Object.fromEntries(entries))
        const first = entries.map(([, p]) => p).find((p) => p !== null)
        if (first) setResolvedFrom(first.from)
      })
      .catch((err: unknown) => {
        if (!cancelled) setPeriodError(errorMessage(err))
      })
      .finally(() => {
        if (!cancelled) setPeriodLoading(false)
      })
    return () => {
      cancelled = true
    }
    // activeBudgets is derived fresh from `budgets` each render; its
    // identity already changes exactly when the underlying list does, so
    // it's the right dependency here rather than `budgets` itself.
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [activeBudgets, anchor])

  function goToPreviousMonth() {
    if (resolvedFrom) setAnchor(shiftMonth(resolvedFrom, -1))
  }

  function goToNextMonth() {
    if (resolvedFrom) setAnchor(shiftMonth(resolvedFrom, 1))
  }

  function openCreate() {
    setEditingBudget(null)
    setDialogOpen(true)
  }

  function openEdit(budget: Budget) {
    setEditingBudget(budget)
    setDialogOpen(true)
  }

  async function handleArchive(budget: Budget) {
    const archiving = archiveBudget(budget.id).then(() => refreshBudgets())
    toast.promise(archiving, {
      loading: 'Archiving…',
      success: 'Budget archived.',
      error: (err) => errorMessage(err),
    })
    try {
      await archiving
    } catch {
      // Failure is already reported via the toast.promise `error` option.
    }
  }

  if (loadError) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 p-12 text-center">
        <p role="alert" className="text-destructive text-sm">
          {loadError}
        </p>
      </div>
    )
  }

  if (budgets === null) {
    return (
      <div className="flex flex-1 flex-col gap-6 p-4 sm:p-6">
        <h1 className="text-2xl font-semibold tracking-tight">Budgets</h1>
        <div className="flex items-center gap-2">
          <Spinner />
          <span className="text-muted-foreground text-sm">Loading…</span>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-1 flex-col gap-6 p-4 sm:p-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Budgets</h1>
          <p className="text-muted-foreground text-sm">
            What you planned to spend per category, against what actually
            happened.
          </p>
        </div>
        {activeBudgets.length > 0 && (
          <Button onClick={openCreate}>New budget</Button>
        )}
      </div>

      {activeBudgets.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <PiggyBank />
            </EmptyMedia>
            <EmptyTitle>No budgets yet</EmptyTitle>
            <EmptyDescription>
              Create a budget to plan an amount per category and see how actual
              spending compares.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button onClick={openCreate}>New budget</Button>
          </EmptyContent>
        </Empty>
      ) : (
        <>
          <div className="flex items-center gap-3">
            <Button
              variant="outline"
              size="sm"
              onClick={goToPreviousMonth}
              disabled={!resolvedFrom}
            >
              Previous
            </Button>
            <span className="min-w-36 text-center text-sm font-medium">
              {resolvedFrom ? formatMonthLabel(resolvedFrom) : 'This month'}
            </span>
            <Button
              variant="outline"
              size="sm"
              onClick={goToNextMonth}
              disabled={!resolvedFrom}
            >
              Next
            </Button>
            {periodLoading && <Spinner className="size-3.5" />}
          </div>

          {periodError && (
            <p role="alert" className="text-destructive text-sm">
              {periodError}
            </p>
          )}

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            {activeBudgets.map((budget) => {
              const period = periodsByBudget[budget.id]
              return (
                <Card key={budget.id} className="lg:col-span-2">
                  <CardHeader className="flex flex-row items-start justify-between gap-2">
                    <div>
                      <CardTitle>{budget.name}</CardTitle>
                      <p className="text-muted-foreground text-xs">
                        {budget.currency}
                      </p>
                    </div>
                    <div className="flex items-center gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => openEdit(budget)}
                      >
                        Edit
                      </Button>
                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={() => handleArchive(budget)}
                      >
                        Archive
                      </Button>
                    </div>
                  </CardHeader>
                  <CardContent>
                    {period === undefined && periodError ? null : period === // failure — nothing more useful to say per card. // The top-level banner above already reports the
                        undefined && periodLoading ? (
                      <div className="flex items-center gap-2">
                        <Spinner className="size-3.5" />
                        <span className="text-muted-foreground text-sm">
                          Loading…
                        </span>
                      </div>
                    ) : period === null || period === undefined ? (
                      <p className="text-muted-foreground text-sm">
                        This budget hadn’t started yet in{' '}
                        {resolvedFrom
                          ? formatMonthLabel(resolvedFrom)
                          : 'this month'}
                        .
                      </p>
                    ) : (
                      <BudgetPeriodTable
                        actuals={period}
                        categoriesByID={categoriesByID}
                      />
                    )}
                  </CardContent>
                </Card>
              )
            })}
          </div>
        </>
      )}

      {dialogOpen && (
        <BudgetDialog
          key={editingBudget?.id ?? 'create'}
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          budget={editingBudget}
          categories={categories}
          onSaved={refreshBudgets}
        />
      )}
    </div>
  )
}
