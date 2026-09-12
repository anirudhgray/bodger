// The Restore subpage of the Import & export area (issue #214), wrapping
// #227's REST surface: upload a canonical JSON backup and replace every
// account, category, and transaction the actor has with its contents.
//
// This is the one flow in the whole web UI where the interface has to
// carry real weight rather than just render a server response: restoring
// has no preview step and cannot be undone (internal/surface/http/restore.go's
// own doc comment — "no preview step: the request must set confirm: true,
// or it's rejected before anything is touched"). So this screen puts a
// destructive-styled warning in front of the action and gates the actual
// request behind typing an exact confirmation phrase, not a single click
// — a mis-click can't fire this the way it could an ordinary "Yes"
// button. The 422 fallback in restoreSnapshotView's error path is exactly
// what a wrong or missing "confirm" produces server-side; this screen's
// job is making sure a real person meant to get there.
import { useState } from 'react'
import { AlertTriangleIcon, UploadIcon } from 'lucide-react'
import { useNavigate } from 'react-router-dom'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { FileDropzone } from '@/components/FileDropzone'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { restoreSnapshot, type RestoreSnapshotResult } from '@/lib/api'
import { errorMessage } from './shared'

// A person has to type this exactly (case-sensitive) before "Replace
// everything" enables. Deliberately not the single-word/"Yes" pattern
// used for reversible actions elsewhere in this app (e.g. archiving an
// account) — see this file's own top comment for why restore earns the
// stricter gate.
const CONFIRM_PHRASE = 'REPLACE ALL MY DATA'

type PickedFile = { name: string; document: unknown }

export function RestorePage() {
  const navigate = useNavigate()
  const [picked, setPicked] = useState<PickedFile | null>(null)
  const [readError, setReadError] = useState<string | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [confirmText, setConfirmText] = useState('')
  const [restoring, setRestoring] = useState(false)
  const [restoreError, setRestoreError] = useState<string | null>(null)
  const [result, setResult] = useState<RestoreSnapshotResult | null>(null)

  async function handleFile(file: File | null) {
    if (!file) return
    setResult(null)
    setReadError(null)
    try {
      const text = await file.text()
      // A syntax check only — confirming this is valid JSON before
      // asking for confirmation, not validating it's actually a
      // bodger.export/v1 document. That validation is the app layer's
      // job (internal/app/restore.go's RestoreSnapshot rejects an
      // unknown or missing format version outright) — duplicating it
      // here would be exactly the second implementation
      // docs/architecture.md §3 rules out.
      const document = JSON.parse(text) as unknown
      setPicked({ name: file.name, document })
    } catch {
      setPicked(null)
      setReadError(
        'That doesn’t look like a valid backup file — it should be the JSON file downloaded from the Export page.',
      )
    }
  }

  function openConfirm() {
    setConfirmText('')
    setRestoreError(null)
    setDialogOpen(true)
  }

  async function handleRestore() {
    if (!picked || confirmText !== CONFIRM_PHRASE) return
    setRestoring(true)
    setRestoreError(null)
    try {
      const restored = await restoreSnapshot(picked.document, true)
      setResult(restored)
      setDialogOpen(false)
      setPicked(null)
    } catch (err) {
      setRestoreError(errorMessage(err))
    } finally {
      setRestoring(false)
    }
  }

  return (
    <section className="flex max-w-2xl flex-col gap-6">
      <Alert variant="destructive">
        <AlertTriangleIcon />
        <AlertTitle>This replaces everything you currently have</AlertTitle>
        <AlertDescription>
          Restoring loads a backup file as your entire ledger: every account,
          category, and transaction you have right now is deleted and replaced
          with the backup’s contents. There is no preview and no undo. If you’re
          not sure, download a fresh backup from Export first.
        </AlertDescription>
      </Alert>

      {result && (
        <Alert>
          <AlertTitle>Restore complete</AlertTitle>
          <AlertDescription>
            Installed {result.accounts} account
            {result.accounts === 1 ? '' : 's'}, {result.categories} categor
            {result.categories === 1 ? 'y' : 'ies'}, and {result.transactions}{' '}
            transaction{result.transactions === 1 ? '' : 's'} from the backup.
          </AlertDescription>
          <Button
            size="sm"
            className="mt-2"
            onClick={() => navigate('/transactions')}
          >
            Go to transactions
          </Button>
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Choose a backup file</CardTitle>
          <CardDescription>
            The canonical JSON file produced by Export’s “Download backup”
            button.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="restore-file">Backup file</Label>
            <FileDropzone
              id="restore-file"
              accept=".json,application/json"
              fileName={picked?.name}
              onFileChange={handleFile}
              prompt="The JSON backup file from Export"
            />
          </div>
          {readError && (
            <p role="alert" className="text-destructive text-sm">
              {readError}
            </p>
          )}
          {picked && (
            <div className="border-border flex flex-wrap items-center justify-between gap-2 rounded-lg border p-3">
              <span className="text-sm">{picked.name}</span>
              <Button variant="destructive" onClick={openConfirm}>
                <UploadIcon /> Replace everything…
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Replace everything with this backup?</DialogTitle>
            <DialogDescription>
              This permanently deletes every account, category, and transaction
              you currently have and replaces them with {picked?.name}’s
              contents. This can’t be undone.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="restore-confirm-phrase">
              Type <span className="font-mono">{CONFIRM_PHRASE}</span> to
              confirm
            </Label>
            <Input
              id="restore-confirm-phrase"
              autoComplete="off"
              autoFocus
              value={confirmText}
              onChange={(event) => setConfirmText(event.target.value)}
            />
          </div>
          {restoreError && (
            <p role="alert" className="text-destructive text-sm">
              {restoreError}
            </p>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={confirmText !== CONFIRM_PHRASE || restoring}
              onClick={handleRestore}
            >
              {restoring ? 'Replacing…' : 'Replace everything'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
