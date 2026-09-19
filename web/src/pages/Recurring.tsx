// The recurring screen (issue #282, M9 · Lothlórien): create/edit/archive
// recurring rules, generate (refresh) their pending occurrences, and
// materialise or skip an upcoming one — the web UI's front end for #281's
// REST surface and #294's GenerateOccurrences wiring.
//
// Three separate entities render here, and they stay visually and
// structurally apart the whole way down (data-model.md §11's "pending
// occurrences are always visually and structurally distinguished from
// actuals," which the issue calls out explicitly): the rules list is its
// own Card, the upcoming-occurrences list is a second, separately-titled
// Card with a dashed border and muted background (not just a status
// badge on an otherwise-ordinary row), and neither ever renders inside
// TransactionsList — a materialised occurrence's resulting Transaction is
// only ever seen there, once it's real. Every rule/occurrence field
// rendered below is exactly what the server returned — this screen
// computes no schedule, no next-fire date, no amount of its own
// (ADR-0009's "the web UI does no maths" rule).
//
// Refresh is a single, explicit, page-level "Refresh occurrences" button
// that calls POST /api/v1/recurring-occurrences/refresh with no rule_id
// (every active rule the actor owns at once — the same default the CLI's
// `bodger recurring refresh` and MCP's refresh_occurrences tool use)
// rather than firing automatically on page load. ADR-0014 requires
// generation to always be "an explicit... action a surface took," and an
// automatic on-load call is a materially weaker reading of that than a
// button someone actually presses — plus a page-load fetch means "click
// this to see the effect" (the flow the issue's own e2e coverage exercises)
// silently already happened, and every screen visit would issue a write
// call with no user intent behind it, on a page an occurrence-materialise
// action already needs to reload anyway.
import { useCallback, useEffect, useMemo, useState } from 'react'
import { CalendarClock, Repeat } from 'lucide-react'
import { toast } from 'sonner'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'
import { RecurringRuleDialog } from '@/components/RecurringRuleDialog'
import {
  ApiError,
  archiveRecurringRule,
  listAccounts,
  listCategories,
  listRecurringRules,
  listScheduledOccurrences,
  materialiseOccurrence,
  refreshOccurrences,
  skipOccurrence,
  type Account,
  type Category,
  type RecurringRule,
  type ScheduledOccurrence,
} from '@/lib/api'
import { scheduleSummary } from '@/lib/schedule'

function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : 'Couldn’t reach the server. Try again.'
}

// signedAmount mirrors TransactionsList.tsx's own +/− prefix for a
// transaction row — a category's kind (never client-derived; read
// straight from the Category the server returned) says which way a
// rule's fixed, always-positive amount moves.
function signedAmount(
  amount: string,
  categoryKind: Category['type'] | undefined,
) {
  const sign =
    categoryKind === 'income' ? '+' : categoryKind === 'expense' ? '−' : ''
  return `${sign}${amount}`
}

function nameFor(id: string, byID: Map<string, string>): string {
  return byID.get(id) ?? id
}

function RuleRow({
  rule,
  accountsByID,
  categoriesByID,
  onEdit,
  onArchive,
}: {
  rule: RecurringRule
  accountsByID: Map<string, string>
  categoriesByID: Map<string, Category>
  onEdit: () => void
  onArchive: () => void
}) {
  const category = categoriesByID.get(rule.category_id)
  return (
    <li className="flex flex-col gap-2 px-4 py-3 text-sm sm:flex-row sm:items-center sm:justify-between">
      <div className="flex flex-col gap-0.5">
        <span className="font-medium">{rule.description}</span>
        <span className="text-muted-foreground text-xs">
          {nameFor(rule.account_id, accountsByID)} ·{' '}
          {category?.name ?? rule.category_id} ·{' '}
          {scheduleSummary(rule.schedule)}
          {rule.ends_on ? ` · ends ${rule.ends_on}` : ''}
        </span>
      </div>
      <div className="flex items-center gap-3 sm:shrink-0">
        <span className="tabular-nums">
          {signedAmount(rule.amount, category?.type)} {rule.currency}
        </span>
        <Button variant="outline" size="sm" onClick={onEdit}>
          Edit
        </Button>
        <Button variant="destructive" size="sm" onClick={onArchive}>
          Archive
        </Button>
      </div>
    </li>
  )
}

function OccurrenceRow({
  occurrence,
  rule,
  accountsByID,
  categoriesByID,
  onMaterialise,
  onSkip,
  busy,
}: {
  occurrence: ScheduledOccurrence
  rule: RecurringRule | undefined
  accountsByID: Map<string, string>
  categoriesByID: Map<string, Category>
  onMaterialise: () => void
  onSkip: () => void
  busy: boolean
}) {
  const category = rule ? categoriesByID.get(rule.category_id) : undefined
  return (
    <li className="flex flex-col gap-2 px-4 py-3 text-sm sm:flex-row sm:items-center sm:justify-between">
      <div className="flex flex-col gap-0.5">
        <div className="flex items-center gap-2">
          <span className="font-medium">
            {rule?.description ?? 'Unknown rule'}
          </span>
          <Badge variant="outline">Projected</Badge>
        </div>
        <span className="text-muted-foreground text-xs">
          {occurrence.occurrence_date}
          {rule && (
            <>
              {' · '}
              {nameFor(rule.account_id, accountsByID)} ·{' '}
              {category?.name ?? rule.category_id}
            </>
          )}
        </span>
      </div>
      <div className="flex items-center gap-3 sm:shrink-0">
        {rule && (
          <span className="text-muted-foreground tabular-nums">
            {signedAmount(rule.amount, category?.type)} {rule.currency}
          </span>
        )}
        <Button
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={onMaterialise}
        >
          Materialise
        </Button>
        <Button variant="ghost" size="sm" disabled={busy} onClick={onSkip}>
          Skip
        </Button>
      </div>
    </li>
  )
}

export function RecurringPage() {
  const [rules, setRules] = useState<RecurringRule[] | null>(null)
  const [occurrences, setOccurrences] = useState<ScheduledOccurrence[] | null>(
    null,
  )
  const [accounts, setAccounts] = useState<Account[]>([])
  const [categories, setCategories] = useState<Category[]>([])
  const [loadError, setLoadError] = useState<string | null>(null)

  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<RecurringRule | null>(null)

  const [refreshing, setRefreshing] = useState(false)
  const [busyOccurrenceId, setBusyOccurrenceId] = useState<string | null>(null)

  const loadAll = useCallback(async () => {
    try {
      const [ruleList, occurrenceList] = await Promise.all([
        listRecurringRules(),
        listScheduledOccurrences({ status: 'pending' }),
      ])
      setRules(ruleList)
      setOccurrences(occurrenceList)
    } catch (err) {
      setLoadError(errorMessage(err))
    }
  }, [])

  useEffect(() => {
    // Fetch-on-mount: everything here reads from the API, an external
    // system that can't be read during render.
    // oxlint-disable-next-line react/set-state-in-effect
    loadAll()
    listAccounts()
      .then(setAccounts)
      .catch(() => {
        // Best-effort, matching Budgets.tsx's own listCategories fallback
        // — a failed load here just means rows show a raw account ID
        // instead of a name, not that the page fails.
      })
    listCategories()
      .then(setCategories)
      .catch(() => {})
  }, [loadAll])

  const activeRules = useMemo(
    () => (rules ?? []).filter((r) => !r.archived),
    [rules],
  )

  const rulesByID = useMemo(
    () => new Map(activeRules.map((r) => [r.id, r])),
    [activeRules],
  )
  const accountsByID = useMemo(
    () => new Map(accounts.map((a) => [a.id, a.name])),
    [accounts],
  )
  const categoriesByID = useMemo(
    () => new Map(categories.map((c) => [c.id, c])),
    [categories],
  )

  function openCreate() {
    setEditingRule(null)
    setDialogOpen(true)
  }

  function openEdit(rule: RecurringRule) {
    setEditingRule(rule)
    setDialogOpen(true)
  }

  async function handleArchive(rule: RecurringRule) {
    const archiving = archiveRecurringRule(rule.id).then(() => loadAll())
    toast.promise(archiving, {
      loading: 'Archiving…',
      success: 'Recurring rule archived.',
      error: (err) => errorMessage(err),
    })
    try {
      await archiving
    } catch {
      // Failure is already reported via the toast.promise `error` option.
    }
  }

  async function handleRefresh() {
    setRefreshing(true)
    try {
      const result = await refreshOccurrences()
      const count = result.created.length
      toast.success(
        count === 0
          ? 'No new occurrences — everything up to the generation horizon already exists.'
          : `Generated ${count} upcoming occurrence${count === 1 ? '' : 's'}.`,
      )
      await loadAll()
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setRefreshing(false)
    }
  }

  async function handleMaterialise(occurrence: ScheduledOccurrence) {
    setBusyOccurrenceId(occurrence.id)
    try {
      await materialiseOccurrence(occurrence.id)
      toast.success('Materialised — recorded as a transaction.')
      await loadAll()
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setBusyOccurrenceId(null)
    }
  }

  async function handleSkip(occurrence: ScheduledOccurrence) {
    setBusyOccurrenceId(occurrence.id)
    try {
      await skipOccurrence(occurrence.id)
      toast.success('Occurrence skipped.')
      await loadAll()
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setBusyOccurrenceId(null)
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

  if (rules === null || occurrences === null) {
    return (
      <div className="flex flex-1 flex-col gap-6 p-4 sm:p-6">
        <h1 className="text-2xl font-semibold tracking-tight">Recurring</h1>
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
          <h1 className="text-2xl font-semibold tracking-tight">Recurring</h1>
          <p className="text-muted-foreground text-sm">
            Rules for transactions that repeat on a schedule, and the upcoming
            occurrences they project — nothing here is a real transaction until
            you materialise it.
          </p>
        </div>
        {/* Mirrors Budgets.tsx's own header: its create button only
            appears once there's a list to act alongside — an empty
            ledger's own Empty-state CTA below is the only "New rule"
            button then, so there's never two competing for the same
            first click. */}
        {activeRules.length > 0 && (
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              onClick={handleRefresh}
              disabled={refreshing}
            >
              {refreshing ? 'Refreshing…' : 'Refresh occurrences'}
            </Button>
            <Button onClick={openCreate}>New rule</Button>
          </div>
        )}
      </div>

      {activeRules.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Repeat />
            </EmptyMedia>
            <EmptyTitle>No recurring rules yet</EmptyTitle>
            <EmptyDescription>
              Create a rule for a transaction that repeats — a subscription,
              rent, or a paycheck — and generate its upcoming occurrences from
              here.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button onClick={openCreate}>New rule</Button>
          </EmptyContent>
        </Empty>
      ) : (
        <>
          <Card className="[--card-spacing:0]">
            <CardHeader className="px-4 pt-4">
              <CardTitle className="text-base">Rules</CardTitle>
            </CardHeader>
            <ul className="divide-border divide-y">
              {activeRules.map((rule) => (
                <RuleRow
                  key={rule.id}
                  rule={rule}
                  accountsByID={accountsByID}
                  categoriesByID={categoriesByID}
                  onEdit={() => openEdit(rule)}
                  onArchive={() => handleArchive(rule)}
                />
              ))}
            </ul>
          </Card>

          {/* Upcoming occurrences: a second, separately-titled Card with a
              dashed border and a muted background — structurally its own
              section, not a status badge tucked into an ordinary row —
              per data-model.md §11's "always visually and structurally
              distinguished from actuals." */}
          <Card className="border-dashed bg-muted/30 [--card-spacing:0]">
            <CardHeader className="px-4 pt-4">
              <CardTitle className="flex items-center gap-2 text-base">
                <CalendarClock className="text-muted-foreground size-4" />
                Upcoming (projected)
              </CardTitle>
            </CardHeader>
            <CardContent className="px-0 pb-4">
              {occurrences.length === 0 ? (
                <p className="text-muted-foreground px-4 text-sm">
                  Nothing pending. Use “Refresh occurrences” above to generate
                  upcoming dates from your active rules.
                </p>
              ) : (
                <ul className="divide-border divide-y">
                  {occurrences.map((occurrence) => (
                    <OccurrenceRow
                      key={occurrence.id}
                      occurrence={occurrence}
                      rule={rulesByID.get(occurrence.rule_id)}
                      accountsByID={accountsByID}
                      categoriesByID={categoriesByID}
                      busy={busyOccurrenceId === occurrence.id}
                      onMaterialise={() => handleMaterialise(occurrence)}
                      onSkip={() => handleSkip(occurrence)}
                    />
                  ))}
                </ul>
              )}
            </CardContent>
          </Card>
        </>
      )}

      {dialogOpen && (
        <RecurringRuleDialog
          key={editingRule?.id ?? 'create'}
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          rule={editingRule}
          accounts={accounts}
          categories={categories}
          onSaved={loadAll}
        />
      )}
    </div>
  )
}
