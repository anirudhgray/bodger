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
  listAccounts,
  listImportBatches,
  listImportRecords,
  resolveImportRecord,
  rollbackImportBatch,
  uploadImport,
  type Account,
  type ImportBatch,
  type ImportBatchStatus,
  type ImportRecord,
} from '@/lib/api'
import { UploadIcon } from 'lucide-react'
import { FileDropzone } from '@/components/FileDropzone'
import { errorMessage } from './shared'

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
  const [batches, setBatches] = useState<ImportBatch[] | null>(null)
  const [historyError, setHistoryError] = useState<string | null>(null)
  const [wizard, setWizard] = useState<Wizard>({ step: 'closed' })
  const [busy, setBusy] = useState(false)

  const accountsByID = new Map(accounts.map((a) => [a.id, a.name]))

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
    // oxlint-disable-next-line react/set-state-in-effect
    loadHistory()
  }, [loadHistory])

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
          pendingCount={pendingCount}
          busy={busy}
          onResolve={handleResolve}
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

function ReviewStep({
  batch,
  records,
  accountName,
  pendingCount,
  busy,
  onResolve,
  onCommit,
  onCancel,
}: {
  batch: ImportBatch
  records: ImportRecord[]
  accountName?: string
  pendingCount: number
  busy: boolean
  onResolve: (
    record: ImportRecord,
    resolution: 'confirmed_duplicate' | 'not_duplicate',
  ) => void
  onCommit: () => void
  onCancel: () => void
}) {
  const alreadyCommitted = batch.status === 'committed'

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
              <TableHead className="text-right">Decision</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((record) => {
              const needsDecision =
                record.status === 'pending' &&
                record.duplicate_match?.tier === 'suspected_duplicate' &&
                record.duplicate_match.resolution === 'pending'
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
                      {record.transfer_candidate_record_id && (
                        <Badge variant="secondary">Possible transfer</Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="text-right">
                    {needsDecision ? (
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
