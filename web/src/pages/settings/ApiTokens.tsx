// The API tokens subpage of Settings (issue #107, split out of the old
// Settings.tsx). No behavior change from the original ApiTokensSection —
// only its own route now, rendered inside SettingsLayout's <Outlet />.
import { KeyRound } from 'lucide-react'
import { useEffect, useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { CopyButton } from '@/components/ui/copy-button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Spinner } from '@/components/ui/spinner'
import {
  createApiToken,
  listApiTokens,
  revokeApiToken,
  type ApiToken,
  type CreatedApiToken,
} from '@/lib/settings'
import { errorMessage } from './shared'

export function ApiTokensSettings() {
  const [tokens, setTokens] = useState<ApiToken[] | null>(null)
  const [name, setName] = useState('')
  const [creating, setCreating] = useState(false)
  const [justCreated, setJustCreated] = useState<CreatedApiToken | null>(null)
  // The listApiTokens() load failure only — create/revoke are
  // user-triggered actions, and report their own failure via toast
  // instead (docs/design-system.md's "Toasts vs. inline messages").
  const [loadError, setLoadError] = useState<string | null>(null)

  async function refresh() {
    try {
      setTokens(await listApiTokens())
    } catch (err) {
      setLoadError(errorMessage(err))
    }
  }

  useEffect(() => {
    // Fetch-on-mount: refresh() calls the list API, an external system
    // that can't be read during render, so an effect is the right tool
    // here — not a value to derive during render or initialize state from.
    // oxlint-disable-next-line react/set-state-in-effect
    refresh()
  }, [])

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCreating(true)
    try {
      const created = await createApiToken(name)
      setJustCreated(created)
      setName('')
      await refresh()
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setCreating(false)
    }
  }

  async function handleRevoke(id: string) {
    // See TransactionsList.tsx's handleDelete for why the action is
    // awaited separately from the toast.promise call.
    const revocation = (async () => {
      await revokeApiToken(id)
      await refresh()
    })()
    toast.promise(revocation, {
      loading: 'Revoking…',
      success: 'Token revoked.',
      error: (err) => errorMessage(err),
    })
    try {
      await revocation
    } catch {
      // Failure is already reported via the toast.promise `error` option
      // above.
    }
  }

  return (
    <section className="flex flex-col gap-4">
      <p className="text-muted-foreground text-sm">
        Tokens authenticate scripts and other external clients. A token's value
        is shown once, when it's created.
      </p>

      <Dialog
        open={justCreated !== null}
        onOpenChange={(open) => {
          if (!open) setJustCreated(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Token created</DialogTitle>
            <DialogDescription>
              Copy <strong>{justCreated?.name}</strong>’s token now — it can’t
              be shown again.
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2">
            <code className="bg-muted flex-1 rounded border px-2 py-1.5 text-sm break-all">
              {justCreated?.token}
            </code>
            <CopyButton value={justCreated?.token ?? ''} label="Copy token" />
          </div>
          <DialogFooter>
            <Button onClick={() => setJustCreated(null)}>
              I’ve copied or stored it
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <form
        onSubmit={handleCreate}
        className="flex max-w-sm flex-wrap items-end gap-2"
      >
        <div className="flex flex-1 flex-col gap-1.5">
          <Label htmlFor="token-name">New token name</Label>
          <Input
            id="token-name"
            required
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </div>
        <Button type="submit" disabled={creating || name === ''}>
          {creating ? 'Creating…' : 'Create'}
        </Button>
      </form>

      {loadError && (
        <p role="alert" className="text-destructive text-sm">
          {loadError}
        </p>
      )}

      {tokens === null ? (
        <div className="flex items-center gap-2">
          <Spinner />
          <span className="text-muted-foreground text-sm">Loading…</span>
        </div>
      ) : tokens.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <KeyRound />
            </EmptyMedia>
            <EmptyTitle>No API tokens yet</EmptyTitle>
          </EmptyHeader>
        </Empty>
      ) : (
        <Card className="[--card-spacing:0]">
          <ul className="divide-border divide-y">
            {tokens.map((token) => (
              <li
                key={token.id}
                className="flex items-center justify-between px-4 py-2.5 text-sm"
              >
                <div>
                  <p className="font-medium">{token.name}</p>
                  <p className="text-muted-foreground text-xs">
                    {token.revoked_at
                      ? 'Revoked'
                      : token.last_used_at
                        ? `Last used ${token.last_used_at}`
                        : 'Never used'}
                  </p>
                </div>
                {!token.revoked_at && (
                  <Button
                    variant="destructive"
                    size="sm"
                    onClick={() => handleRevoke(token.id)}
                  >
                    Revoke
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </Card>
      )}
    </section>
  )
}
