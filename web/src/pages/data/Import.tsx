// The Import subpage of the Import & export area (issue #214), wrapping
// #212's REST surface for ADR-0008's staged pipeline: upload a file,
// review what it staged (duplicates and transfer candidates flagged, not
// auto-resolved), commit it, and roll a committed import back later from
// its own history. Nothing here writes a transaction directly — every
// mutation is a call to an existing app-layer use case via lib/api.ts;
// this file only sequences the screen through that pipeline's own stages
// (docs/architecture.md §3, "no financial logic in the web UI").
import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Label } from '@/components/ui/label'
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
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
  commitImportBatch,
  getImportBatch,
  getImportSuggestions,
  listAccounts,
  listCategories,
  listImportBatches,
  listImportRecords,
  resolveImportRecord,
  resolveImportRecordOccurrenceMatch,
  rollbackImportBatch,
  uploadImport,
  type Account,
  type Category,
  type ImportBatch,
  type ImportBatchStatus,
  type ImportRecord,
  type ImportRowSuggestion,
  type ImportSuggestions,
  type ResolvableOccurrenceMatchResolution,
} from '@/lib/api'
import { Sparkles, UploadIcon } from 'lucide-react'
import { FileDropzone } from '@/components/FileDropzone'
import { errorMessage } from './shared'

// explainSuggestionFailure mirrors internal/surface/cli/import.go's own
// explainFailureReason — the same plain-English clauses for the same
// closed set of reason codes, reimplemented here rather than shared
// because the CLI's Go can't run in the web bundle (docs/ux-principles.md
// §2: a surface may earn its own spelling of the same fact, as long as
// two independently-built surfaces don't silently drift — this is that
// divergence point, stated). A reason this hasn't been taught yet falls
// back to a generic clause rather than leaking the raw code.
function explainSuggestionFailure(reason?: string): string {
  switch (reason) {
    case 'credential_rejected':
      return 'your typesafe.ai credential was rejected'
    case 'throttled':
      return 'typesafe.ai is rate-limiting this instance right now'
    case 'provider_unreachable':
      return "typesafe.ai couldn't be reached"
    default:
      return "typesafe.ai didn't answer"
  }
}

function batchStatusLabel(status: ImportBatchStatus): string {
  switch (status) {
    case 'staged':
      return 'Staged'
    case 'reviewed':
      return 'Reviewed'
    case 'committed':
      return 'Committed'
    case 'rolled_back':
      return 'Rolled back'
  }
}

function batchStatusVariant(
  status: ImportBatchStatus,
): 'secondary' | 'default' | 'outline' {
  switch (status) {
    case 'committed':
      return 'default'
    case 'rolled_back':
      return 'outline'
    default:
      return 'secondary'
  }
}

// Read only far enough to split off the header row — this is a display
// convenience for the mapping step's dropdowns (so a person picks "Debit
// Amount" from a list instead of typing it by hand), not the import
// parser: internal/app/importparse's CSVParser is what actually reads and
// normalises every row once the file is uploaded (ADR-0008 — "a parser's
// only job is to turn bytes into normalised ImportRecord fields"). A
// header this misreads (an embedded comma inside a quoted title, say)
// only means a less helpful guess in these dropdowns; the file itself is
// uploaded and parsed server-side unchanged either way.
function parseHeaderRow(text: string): string[] {
  const firstLine = text.split(/\r\n|\r|\n/, 1)[0] ?? ''
  return firstLine
    .split(',')
    .map((h) => h.trim().replace(/^"(.*)"$/, '$1'))
    .filter((h) => h.length > 0)
}

function guessColumn(headers: string[], keywords: string[]): string {
  const lower = headers.map((h) => h.toLowerCase())
  for (const keyword of keywords) {
    const idx = lower.findIndex((h) => h.includes(keyword))
    if (idx !== -1) return headers[idx]
  }
  return ''
}

// The wizard's own step indicator (a labeled Progress bar, not a
// separate "wizard" or "stepper" primitive — shadcn/ui's default
// registry, the only one this project's components.json points at,
// doesn't ship one). Three fixed steps rather than a generic N-step
// component: this wizard's shape is fixed by the pipeline it wraps
// (ADR-0008's upload → mapping → review stages), so a configurable
// stepper would be generality this screen has no second caller for.
const WIZARD_STEPS = ['Upload', 'Map columns', 'Review'] as const

function wizardStepIndex(step: Wizard['step']): number {
  switch (step) {
    case 'upload':
      return 0
    case 'mapping':
      return 1
    case 'review':
      return 2
    case 'closed':
      return -1
  }
}

function WizardProgress({ step }: { step: Wizard['step'] }) {
  const index = wizardStepIndex(step)
  if (index === -1) return null
  return (
    <div className="flex max-w-2xl flex-col gap-1.5">
      <div className="text-muted-foreground flex justify-between text-xs">
        {WIZARD_STEPS.map((label, i) => (
          <span
            key={label}
            className={i === index ? 'text-foreground font-medium' : ''}
          >
            {label}
          </span>
        ))}
      </div>
      <Progress value={((index + 1) / WIZARD_STEPS.length) * 100} />
    </div>
  )
}

const NONE_VALUE = 'none'

type ColumnMappingState = {
  dateColumn: string
  descriptionColumn: string
  amountColumn: string
  postedDateColumn: string
  currencyColumn: string
  externalIDColumn: string
  categoryColumn: string
}

function guessMapping(headers: string[]): ColumnMappingState {
  return {
    dateColumn: guessColumn(headers, ['booked date', 'date']),
    descriptionColumn: guessColumn(headers, [
      'description',
      'memo',
      'payee',
      'narrative',
    ]),
    amountColumn: guessColumn(headers, ['amount']),
    postedDateColumn: guessColumn(headers, ['posted']),
    currencyColumn: guessColumn(headers, ['currency']),
    externalIDColumn: guessColumn(headers, [
      'transaction id',
      'reference',
      'id',
    ]),
    categoryColumn: guessColumn(headers, ['category']),
  }
}

// Wizard is the in-progress "new import" flow's own state machine — a
// discriminated union rather than several independent booleans/nullables,
// so a given render can only ever be in exactly one of these shapes.
type Wizard =
  | { step: 'closed' }
  | { step: 'upload' }
  | {
      step: 'mapping'
      account: string
      file: File
      fileText: string
      headers: string[]
      mapping: ColumnMappingState
    }
  | { step: 'review'; batch: ImportBatch; records: ImportRecord[] }

export function ImportPage() {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [categories, setCategories] = useState<Category[]>([])
  const [batches, setBatches] = useState<ImportBatch[] | null>(null)
  const [historyError, setHistoryError] = useState<string | null>(null)
  const [wizard, setWizard] = useState<Wizard>({ step: 'closed' })
  const [busy, setBusy] = useState(false)
  const [suggestions, setSuggestions] = useState<ImportSuggestions | null>(null)
  // suggestionsLoading distinguishes "the request is in flight" from
  // "loaded, and there's genuinely nothing to show" (not-configured,
  // failed, or every row already resolved) — suggestions alone collapses
  // both to null, which is what let the Suggested column render nothing,
  // then pop in cards once the fetch resolved, shifting every row's
  // height with no loading affordance in between.
  const [suggestionsLoading, setSuggestionsLoading] = useState(false)
  const [busyOccurrenceRecordId, setBusyOccurrenceRecordId] = useState<
    string | null
  >(null)

  const accountsByID = new Map(accounts.map((a) => [a.id, a.name]))
  const categoriesByID = new Map(categories.map((c) => [c.id, c]))

  const loadHistory = useCallback(async () => {
    try {
      setBatches(await listImportBatches())
      setHistoryError(null)
    } catch (err) {
      setHistoryError(errorMessage(err))
    }
  }, [])

  useEffect(() => {
    // oxlint-disable-next-line react/set-state-in-effect
    listAccounts()
      .then(setAccounts)
      .catch((err: unknown) => toast.error(errorMessage(err)))
    // Best-effort, matching Recurring.tsx's own listCategories fallback —
    // a failed load here just means suggestion cards show a raw category
    // ID instead of a name, not that the page fails.
    // oxlint-disable-next-line react/set-state-in-effect
    listCategories()
      .then(setCategories)
      .catch(() => {})
    // oxlint-disable-next-line react/set-state-in-effect
    loadHistory()
  }, [loadHistory])

  // Fetch suggestions exactly once per review — when the review step is
  // entered for a given batch, not on every render/keystroke (#307's own
  // "call it once when the review step loads" requirement) — and never on
  // the upload/mapping steps, since a suggestion request is a metered
  // typesafe.ai call and those steps have no staged rows yet to ask about.
  // Suggestions are advisory only, so a failure to even reach this
  // instance's own suggestions endpoint is swallowed rather than shown:
  // ADR-0015's "a network error must never become an error page over data
  // sitting in SQLite" applies here the same as any other import-review
  // failure. The endpoint's own configured/failed states (as opposed to a
  // transport failure calling it) are rendered further down, exactly as
  // returned.
  const reviewBatchId = wizard.step === 'review' ? wizard.batch.id : null
  useEffect(() => {
    if (reviewBatchId === null) return
    let cancelled = false
    // oxlint-disable-next-line react/set-state-in-effect
    setSuggestions(null)
    // oxlint-disable-next-line react/set-state-in-effect
    setSuggestionsLoading(true)
    getImportSuggestions(reviewBatchId)
      .then((result) => {
        if (!cancelled) setSuggestions(result)
      })
      .catch(() => {
        if (!cancelled) setSuggestions(null)
      })
      .finally(() => {
        if (!cancelled) setSuggestionsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [reviewBatchId])

  async function handleFileChosen(account: string, file: File) {
    setBusy(true)
    try {
      const fileText = await file.text()
      const headers = parseHeaderRow(fileText)
      setWizard({
        step: 'mapping',
        account,
        file,
        fileText,
        headers,
        mapping: guessMapping(headers),
      })
    } catch {
      toast.error('Couldn’t read that file. Try a plain CSV export.')
    } finally {
      setBusy(false)
    }
  }

  async function handleStage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (wizard.step !== 'mapping') return
    const { account, file, fileText, mapping } = wizard
    setBusy(true)
    try {
      const result = await uploadImport({
        account,
        filename: file.name,
        fileContent: fileText,
        mapping: {
          dateColumn: mapping.dateColumn,
          descriptionColumn: mapping.descriptionColumn,
          amountColumn: mapping.amountColumn,
          postedDateColumn: mapping.postedDateColumn || undefined,
          currencyColumn: mapping.currencyColumn || undefined,
          externalIDColumn: mapping.externalIDColumn || undefined,
          categoryColumn: mapping.categoryColumn || undefined,
        },
      })
      setWizard({
        step: 'review',
        batch: result.batch,
        records: result.records,
      })
      await loadHistory()
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  async function handleResolve(
    record: ImportRecord,
    resolution: 'confirmed_duplicate' | 'not_duplicate',
  ) {
    if (wizard.step !== 'review') return
    try {
      const updated = await resolveImportRecord(record.id, resolution)
      setWizard({
        step: 'review',
        batch: wizard.batch,
        records: wizard.records.map((r) => (r.id === updated.id ? updated : r)),
      })
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  // handleResolveOccurrenceMatch is the gap-closing action #309/#312
  // shipped app/CLI/HTTP for but never a web UI (issue #307's own
  // user-confirmed scope note): resolving a row's deterministic
  // OccurrenceMatch. It's also exactly the same call an AI-suggested
  // occurrence match reaches to "accept" a suggestion — there is no
  // second write path for that — which is why occurrenceId is accepted
  // here too: required only when record has no OccurrenceMatch of its
  // own already (an AI-only suggestion a manual test against a live
  // typesafe.ai key found silently doing nothing before this parameter
  // existed — see resolveImportRecordOccurrenceMatch's own doc comment,
  // api.ts), ignored otherwise.
  async function handleResolveOccurrenceMatch(
    record: ImportRecord,
    resolution: ResolvableOccurrenceMatchResolution,
    occurrenceId?: string,
  ) {
    if (wizard.step !== 'review') return
    setBusyOccurrenceRecordId(record.id)
    try {
      const updated = await resolveImportRecordOccurrenceMatch(
        record.id,
        resolution,
        occurrenceId,
      )
      setWizard({
        step: 'review',
        batch: wizard.batch,
        records: wizard.records.map((r) => (r.id === updated.id ? updated : r)),
      })
      if (resolution === 'materialized') {
        toast.success('Materialised — recorded as a transaction.')
      }
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setBusyOccurrenceRecordId(null)
    }
  }

  async function handleCommit() {
    if (wizard.step !== 'review') return
    const batchId = wizard.batch.id
    setBusy(true)
    const committing = (async () => {
      const result = await commitImportBatch(batchId)
      setWizard({ step: 'closed' })
      await loadHistory()
      return result
    })()
    toast.promise(committing, {
      loading: 'Committing…',
      success: (result) =>
        `Import committed — ${result.transactions.length} transaction${
          result.transactions.length === 1 ? '' : 's'
        } created.`,
      error: (err) => errorMessage(err),
    })
    try {
      await committing
    } catch {
      // Reported via the toast.promise error option above.
    } finally {
      setBusy(false)
    }
  }

  async function handleContinueReview(batchId: string) {
    try {
      const [batch, records] = await Promise.all([
        getImportBatch(batchId),
        listImportRecords(batchId),
      ])
      setWizard({ step: 'review', batch, records })
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  async function handleRollback(batchId: string) {
    const rollingBack = (async () => {
      await rollbackImportBatch(batchId)
      await loadHistory()
    })()
    toast.promise(rollingBack, {
      loading: 'Rolling back…',
      success: 'Import rolled back — its transactions were deleted.',
      error: (err) => errorMessage(err),
    })
    try {
      await rollingBack
    } catch {
      // Reported via the toast.promise error option above.
    }
  }

  const pendingCount =
    wizard.step === 'review'
      ? wizard.records.filter((r) => r.status === 'pending').length
      : 0

  return (
    <section className="flex flex-col gap-6">
      <p className="text-muted-foreground text-sm">
        Bring in transactions from a bank or card statement. Nothing posts to
        your ledger until you review and commit — every committed import can be
        rolled back afterward.
      </p>

      {wizard.step === 'closed' && (
        <div>
          <Button onClick={() => setWizard({ step: 'upload' })}>
            <UploadIcon /> New import
          </Button>
        </div>
      )}

      <WizardProgress step={wizard.step} />

      {wizard.step === 'upload' && (
        <UploadStep
          accounts={accounts}
          busy={busy}
          onChoose={handleFileChosen}
          onCancel={() => setWizard({ step: 'closed' })}
        />
      )}

      {wizard.step === 'mapping' && (
        <MappingStep
          wizard={wizard}
          busy={busy}
          onChange={(mapping) => setWizard({ ...wizard, mapping })}
          onSubmit={handleStage}
          onCancel={() => setWizard({ step: 'closed' })}
        />
      )}

      {wizard.step === 'review' && (
        <ReviewStep
          batch={wizard.batch}
          records={wizard.records}
          accountName={accountsByID.get(wizard.batch.target_account_id)}
          categoriesByID={categoriesByID}
          pendingCount={pendingCount}
          busy={busy}
          onResolve={handleResolve}
          onResolveOccurrenceMatch={handleResolveOccurrenceMatch}
          busyOccurrenceRecordId={busyOccurrenceRecordId}
          suggestions={suggestions}
          suggestionsLoading={suggestionsLoading}
          onCommit={handleCommit}
          onCancel={() => setWizard({ step: 'closed' })}
        />
      )}

      <div className="flex flex-col gap-3">
        <h2 className="text-lg font-medium">Import history</h2>
        {historyError && (
          <p role="alert" className="text-destructive text-sm">
            {historyError}
          </p>
        )}
        {batches === null ? (
          <div className="flex items-center gap-2">
            <Spinner />
            <span className="text-muted-foreground text-sm">Loading…</span>
          </div>
        ) : batches.length === 0 ? (
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <UploadIcon />
              </EmptyMedia>
              <EmptyTitle>No imports yet</EmptyTitle>
              <EmptyDescription>
                Start a new import above to bring in transactions from a file.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Card className="[--card-spacing:0]">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>File</TableHead>
                  <TableHead>Account</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {batches.map((batch) => (
                  <TableRow key={batch.id}>
                    <TableCell>{batch.filename}</TableCell>
                    <TableCell>
                      {accountsByID.get(batch.target_account_id) ??
                        batch.target_account_id}
                    </TableCell>
                    <TableCell>
                      <Badge variant={batchStatusVariant(batch.status)}>
                        {batchStatusLabel(batch.status)}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        {(batch.status === 'staged' ||
                          batch.status === 'reviewed') && (
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => handleContinueReview(batch.id)}
                          >
                            Continue review
                          </Button>
                        )}
                        {batch.status === 'committed' && (
                          <AlertDialog>
                            <AlertDialogTrigger asChild>
                              <Button variant="destructive" size="sm">
                                Roll back
                              </Button>
                            </AlertDialogTrigger>
                            <AlertDialogContent>
                              <AlertDialogHeader>
                                <AlertDialogTitle>
                                  Roll back this import?
                                </AlertDialogTitle>
                                <AlertDialogDescription>
                                  This deletes every transaction “
                                  {batch.filename}” created. They keep their
                                  history, the same as deleting any other
                                  transaction.
                                </AlertDialogDescription>
                              </AlertDialogHeader>
                              <AlertDialogFooter>
                                <AlertDialogCancel>Cancel</AlertDialogCancel>
                                <AlertDialogAction
                                  variant="destructive"
                                  onClick={() => handleRollback(batch.id)}
                                >
                                  Roll back
                                </AlertDialogAction>
                              </AlertDialogFooter>
                            </AlertDialogContent>
                          </AlertDialog>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>
        )}
      </div>
    </section>
  )
}

function UploadStep({
  accounts,
  busy,
  onChoose,
  onCancel,
}: {
  accounts: Account[]
  busy: boolean
  onChoose: (account: string, file: File) => void
  onCancel: () => void
}) {
  const [account, setAccount] = useState('')
  const [file, setFile] = useState<File | null>(null)

  return (
    <Card className="max-w-lg">
      <CardHeader>
        <CardTitle>Upload a file</CardTitle>
        <CardDescription>
          Pick the account it's for and a CSV export from your bank or card.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="import-account">Account</Label>
          <Select value={account} onValueChange={setAccount}>
            <SelectTrigger id="import-account">
              <SelectValue placeholder="Which account is this for?" />
            </SelectTrigger>
            <SelectContent>
              {accounts.map((a) => (
                <SelectItem key={a.id} value={a.id}>
                  {a.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="import-file">File</Label>
          <FileDropzone
            id="import-file"
            accept=".csv,text/csv"
            fileName={file?.name}
            onFileChange={setFile}
            prompt="A CSV export from your bank or card"
            hint="Only CSV is supported today."
          />
        </div>
      </CardContent>
      <CardFooter className="gap-2">
        <Button
          disabled={!account || !file || busy}
          onClick={() => file && onChoose(account, file)}
        >
          {busy ? 'Reading…' : 'Continue'}
        </Button>
        <Button variant="outline" onClick={onCancel}>
          Cancel
        </Button>
      </CardFooter>
    </Card>
  )
}

function MappingStep({
  wizard,
  busy,
  onChange,
  onSubmit,
  onCancel,
}: {
  wizard: Extract<Wizard, { step: 'mapping' }>
  busy: boolean
  onChange: (mapping: ColumnMappingState) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onCancel: () => void
}) {
  const { headers, mapping } = wizard

  function columnSelect(
    id: string,
    label: string,
    field: keyof ColumnMappingState,
    required: boolean,
  ) {
    return (
      <div className="flex flex-col gap-1.5">
        <Label htmlFor={id}>
          {label}
          {required ? '' : ' (optional)'}
        </Label>
        <Select
          value={mapping[field] === '' ? NONE_VALUE : mapping[field]}
          onValueChange={(value) =>
            onChange({
              ...mapping,
              [field]: value === NONE_VALUE ? '' : value,
            })
          }
        >
          <SelectTrigger id={id}>
            <SelectValue placeholder="Choose a column" />
          </SelectTrigger>
          <SelectContent>
            {!required && <SelectItem value={NONE_VALUE}>None</SelectItem>}
            {headers.map((h) => (
              <SelectItem key={h} value={h}>
                {h}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    )
  }

  const canSubmit =
    mapping.dateColumn !== '' &&
    mapping.descriptionColumn !== '' &&
    mapping.amountColumn !== ''

  return (
    <Card className="max-w-2xl">
      <CardHeader>
        <CardTitle>{wizard.file.name}</CardTitle>
        <CardDescription>
          Match each field to the right column from your file.
        </CardDescription>
      </CardHeader>
      <form onSubmit={onSubmit}>
        <CardContent>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {columnSelect('map-date', 'Date', 'dateColumn', true)}
            {columnSelect(
              'map-description',
              'Description',
              'descriptionColumn',
              true,
            )}
            {columnSelect('map-amount', 'Amount', 'amountColumn', true)}
            {columnSelect(
              'map-posted-date',
              'Posted date',
              'postedDateColumn',
              false,
            )}
            {columnSelect('map-currency', 'Currency', 'currencyColumn', false)}
            {columnSelect(
              'map-external-id',
              'Transaction ID',
              'externalIDColumn',
              false,
            )}
            {columnSelect('map-category', 'Category', 'categoryColumn', false)}
          </div>
        </CardContent>
        <CardFooter className="mt-4 gap-2">
          <Button type="submit" disabled={!canSubmit || busy}>
            {busy ? 'Staging…' : 'Stage for review'}
          </Button>
          <Button type="button" variant="outline" onClick={onCancel}>
            Cancel
          </Button>
        </CardFooter>
      </form>
    </Card>
  )
}

function duplicateBadge(record: ImportRecord) {
  const match = record.duplicate_match
  if (!match) return null
  if (match.tier === 'exact') {
    return <Badge variant="outline">Duplicate — skipped</Badge>
  }
  if (match.resolution === 'confirmed_duplicate') {
    return <Badge variant="outline">Confirmed duplicate</Badge>
  }
  if (match.resolution === 'not_duplicate') {
    return <Badge variant="secondary">Not a duplicate</Badge>
  }
  return <Badge variant="destructive">Suspected duplicate</Badge>
}

// occurrenceMatchBadge mirrors duplicateBadge's own pattern one field
// over, for issue #309/#312's OccurrenceMatch — a row whose date and
// amount already look like a specific pending recurring occurrence.
function occurrenceMatchBadge(record: ImportRecord) {
  const match = record.occurrence_match
  if (!match) return null
  if (match.resolution === 'materialized') {
    return <Badge variant="outline">Occurrence materialised</Badge>
  }
  if (match.resolution === 'dismissed') {
    return <Badge variant="secondary">Not this occurrence</Badge>
  }
  return <Badge variant="destructive">Matches a pending occurrence</Badge>
}

// SuggestionCell renders #307's advisory typesafe.ai suggestions for one
// row, attributed by name (ADR-0015, mirroring Balances.tsx's own
// "source: frankfurter" treatment) and never showing a raw confidence
// decimal (docs/ux-principles.md §6's progressive-disclosure rule — the
// REST response is already ranked by confidence, best first, so nothing
// here needs to re-sort or print the number).
//
// Purely informational — neither suggestion kind carries its own accept
// control here. A category suggestion never has: there is genuinely no
// write endpoint for "assign this category to a staged row" (#305/#306),
// so accepting one means committing the import and setting the category
// yourself afterward, stated in the card's own copy. An occurrence
// suggestion *does* have a real write to make (the same
// resolveImportRecordOccurrenceMatch call a deterministic match's own
// Materialise button reaches), but the button for it lives in the
// Decision column instead, alongside every other per-row decision this
// screen asks for — a first version of this screen put an "Accept match"
// button here too, which put decisions in two different places for no
// good reason (an occurrence suggestion this screen already deterministically
// matched would show both a Decision-column Materialise button and this
// column's own Accept button for the same action) and, in manual
// testing, wasn't even wired to the right occurrence id. See
// ReviewStep's needsOccurrenceDecision for where that action now lives.
function SuggestionCell({
  suggestion,
  categoriesByID,
  tooManyCategoryOptions,
}: {
  suggestion: ImportRowSuggestion | undefined
  categoriesByID: Map<string, Category>
  tooManyCategoryOptions: boolean
}) {
  if (!suggestion && !tooManyCategoryOptions) {
    return <span className="text-muted-foreground text-sm">—</span>
  }

  return (
    <div className="flex flex-col gap-1.5">
      {suggestion?.category && (
        <div className="flex flex-col gap-0.5 rounded-md border bg-muted/30 px-2 py-1.5 text-xs">
          <span className="flex items-center gap-1 font-medium">
            <Sparkles className="text-muted-foreground size-3" />
            {categoriesByID.get(suggestion.category.category_id)?.name ??
              suggestion.category.category_id}
          </span>
          <span className="text-muted-foreground">
            Suggested by typesafe.ai — set it when you commit, or edit the
            transaction afterward.
          </span>
        </div>
      )}
      {suggestion?.occurrence && (
        <div className="flex flex-col items-start gap-1 rounded-md border border-dashed bg-muted/30 px-2 py-1.5 text-xs">
          <Badge variant="outline">Projected match</Badge>
          <span className="text-muted-foreground">
            Suggested by typesafe.ai — not settled money until accepted in the
            Decision column.
          </span>
        </div>
      )}
      {tooManyCategoryOptions && (
        <span className="text-muted-foreground text-xs">
          Too many categories of this kind to suggest from.
        </span>
      )}
    </div>
  )
}

// SuggestionCellSkeleton renders while suggestions are still loading
// (ReviewStep's own suggestionsLoading) — a fixed-size placeholder so the
// Suggested column doesn't visibly pop from "—" to a real suggestion card
// and shift every row's height once the request resolves.
function SuggestionCellSkeleton() {
  return <Skeleton className="h-11 w-44 rounded-md" />
}

function ReviewStep({
  batch,
  records,
  accountName,
  categoriesByID,
  pendingCount,
  busy,
  onResolve,
  onResolveOccurrenceMatch,
  busyOccurrenceRecordId,
  suggestions,
  suggestionsLoading,
  onCommit,
  onCancel,
}: {
  batch: ImportBatch
  records: ImportRecord[]
  accountName?: string
  categoriesByID: Map<string, Category>
  pendingCount: number
  busy: boolean
  onResolve: (
    record: ImportRecord,
    resolution: 'confirmed_duplicate' | 'not_duplicate',
  ) => void
  onResolveOccurrenceMatch: (
    record: ImportRecord,
    resolution: ResolvableOccurrenceMatchResolution,
    occurrenceId?: string,
  ) => void
  busyOccurrenceRecordId: string | null
  suggestions: ImportSuggestions | null
  suggestionsLoading: boolean
  onCommit: () => void
  onCancel: () => void
}) {
  const alreadyCommitted = batch.status === 'committed'

  const suggestionByRecordID = new Map(
    (suggestions?.suggestions ?? []).map((s) => [s.record_id, s]),
  )
  const tooManyCategoryOptions = new Set(
    suggestions?.too_many_category_options ?? [],
  )

  // ADR-0015's not-configured/failed states: one quiet, non-blocking line
  // where the suggestion affordance would be — never a modal, never a
  // banner elsewhere, and the review screen below stays exactly as
  // usable either way, since every row still renders regardless.
  let suggestionNotice: string | null = null
  if (suggestions && !suggestions.configured) {
    suggestionNotice = 'AI-assisted suggestions aren’t set up on this instance.'
  } else if (suggestions && suggestions.rows_failed > 0) {
    suggestionNotice = `Couldn’t get suggestions from typesafe.ai for ${
      suggestions.rows_failed
    } row${suggestions.rows_failed === 1 ? '' : 's'} — ${explainSuggestionFailure(
      suggestions.failure_reason,
    )}. Every row below is still fully reviewable.`
  }

  return (
    <Card>
      <CardHeader className="flex-row flex-wrap items-center justify-between gap-2 space-y-0">
        <div>
          <CardTitle>{batch.filename}</CardTitle>
          <CardDescription>
            {accountName ?? batch.target_account_id} · {records.length} row
            {records.length === 1 ? '' : 's'}
            {pendingCount > 0 && ` · ${pendingCount} awaiting your decision`}
          </CardDescription>
          {suggestionNotice && (
            <p className="text-muted-foreground mt-1 text-xs">
              {suggestionNotice}
            </p>
          )}
        </div>
        {!alreadyCommitted && (
          <div className="flex gap-2">
            <Button
              onClick={onCommit}
              disabled={pendingCount > 0 || busy || records.length === 0}
            >
              {busy ? 'Committing…' : 'Commit import'}
            </Button>
            <Button variant="outline" onClick={onCancel}>
              Do this later
            </Button>
          </div>
        )}
      </CardHeader>

      <CardContent className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Date</TableHead>
              <TableHead>Description</TableHead>
              <TableHead className="text-right">Amount</TableHead>
              <TableHead>Flags</TableHead>
              <TableHead>Suggested</TableHead>
              <TableHead className="text-right">Decision</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((record) => {
              const needsDuplicateDecision =
                record.status === 'pending' &&
                record.duplicate_match?.tier === 'suspected_duplicate' &&
                record.duplicate_match.resolution === 'pending'
              const needsDeterministicOccurrenceDecision =
                record.status === 'pending' &&
                record.occurrence_match?.resolution === 'pending'
              // An AI-suggested occurrence match this row's own
              // deterministic detection missed (findOccurrenceMatch's
              // staging-time gate additionally requires description
              // similarity, which SuggestForImportBatch's own candidate
              // narrowing deliberately skips) — the row cleared review as
              // ready, with no OccurrenceMatch attached at all, so this
              // is the *only* place the decision is ever offered. Never
              // shown once record.occurrence_match exists already
              // (needsDeterministicOccurrenceDecision covers that row
              // instead, once), and never once the row has committed or
              // excluded — accepting it then would materialise a second,
              // genuinely double-counted transaction (the same guard the
              // backend itself enforces).
              const aiOccurrenceSuggestion = suggestionByRecordID.get(
                record.id,
              )?.occurrence
              const needsAIOnlyOccurrenceDecision =
                record.status === 'ready' &&
                !record.occurrence_match &&
                !!aiOccurrenceSuggestion
              const needsOccurrenceDecision =
                needsDeterministicOccurrenceDecision ||
                needsAIOnlyOccurrenceDecision
              const occurrenceBusy = busyOccurrenceRecordId === record.id
              return (
                <TableRow key={record.id}>
                  <TableCell>{record.booked_date}</TableCell>
                  <TableCell>{record.description}</TableCell>
                  <TableCell className="text-right font-mono">
                    {record.amount} {record.currency}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      {duplicateBadge(record)}
                      {occurrenceMatchBadge(record)}
                      {record.transfer_candidate_record_id && (
                        <Badge variant="secondary">Possible transfer</Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell>
                    {suggestionsLoading ? (
                      <SuggestionCellSkeleton />
                    ) : (
                      <SuggestionCell
                        suggestion={suggestionByRecordID.get(record.id)}
                        categoriesByID={categoriesByID}
                        tooManyCategoryOptions={tooManyCategoryOptions.has(
                          record.id,
                        )}
                      />
                    )}
                  </TableCell>
                  <TableCell className="text-right">
                    {needsDuplicateDecision || needsOccurrenceDecision ? (
                      <div className="flex flex-col items-end gap-2">
                        {needsDuplicateDecision && (
                          <div className="flex justify-end gap-2">
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => onResolve(record, 'not_duplicate')}
                            >
                              Not a duplicate
                            </Button>
                            <Button
                              size="sm"
                              variant="destructive"
                              onClick={() =>
                                onResolve(record, 'confirmed_duplicate')
                              }
                            >
                              Confirm duplicate
                            </Button>
                          </div>
                        )}
                        {needsOccurrenceDecision && (
                          <div className="flex justify-end gap-2">
                            <Button
                              size="sm"
                              variant="ghost"
                              disabled={occurrenceBusy}
                              onClick={() =>
                                onResolveOccurrenceMatch(
                                  record,
                                  'dismissed',
                                  needsAIOnlyOccurrenceDecision
                                    ? aiOccurrenceSuggestion?.occurrence_id
                                    : undefined,
                                )
                              }
                            >
                              Not this occurrence
                            </Button>
                            <Button
                              size="sm"
                              variant="outline"
                              disabled={occurrenceBusy}
                              onClick={() =>
                                onResolveOccurrenceMatch(
                                  record,
                                  'materialized',
                                  needsAIOnlyOccurrenceDecision
                                    ? aiOccurrenceSuggestion?.occurrence_id
                                    : undefined,
                                )
                              }
                            >
                              Materialise
                            </Button>
                          </div>
                        )}
                      </div>
                    ) : (
                      <span className="text-muted-foreground text-sm">—</span>
                    )}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
