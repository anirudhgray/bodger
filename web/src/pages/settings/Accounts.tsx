// The Accounts subpage of Settings (issue #107, split out of the old
// Settings.tsx). No behavior change from the original AccountsSection —
// only its own route now, rendered inside SettingsLayout's <Outlet />.
import { useEffect, useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { type Account, type AccountKind } from '@/lib/api'
import {
  archiveAccount,
  createAccount,
  listAccounts,
  renameAccount,
} from '@/lib/settings'
import { errorMessage } from './shared'

// accountKindLabel mirrors TransactionsList.tsx's kindLabel: the wire
// vocabulary (AccountKind's snake_case enum) is never what a person reads
// — "one vocabulary, not two" across every surface (docs/ux-principles.md
// §7).
function accountKindLabel(kind: AccountKind): string {
  switch (kind) {
    case 'bank':
      return 'Bank'
    case 'cash':
      return 'Cash'
    case 'credit_card':
      return 'Credit card'
    case 'wallet':
      return 'Wallet'
    case 'investment':
      return 'Investment'
    case 'loan':
      return 'Loan'
    case 'other':
      return 'Other'
  }
}

const ACCOUNT_TYPES: AccountKind[] = [
  'bank',
  'cash',
  'credit_card',
  'wallet',
  'investment',
  'loan',
  'other',
]

export function AccountsSettings() {
  const [accounts, setAccounts] = useState<Account[] | null>(null)
  const [name, setName] = useState('')
  const [type, setType] = useState(ACCOUNT_TYPES[0])
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [renamingID, setRenamingID] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')

  async function refresh() {
    try {
      setAccounts(await listAccounts())
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  useEffect(() => {
    refresh()
  }, [])

  async function handleCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setCreating(true)
    try {
      await createAccount({ name, type })
      setName('')
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setCreating(false)
    }
  }

  async function handleRename(id: string) {
    setError(null)
    try {
      await renameAccount(id, renameValue)
      setRenamingID(null)
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  async function handleArchive(id: string) {
    setError(null)
    // See TransactionsList.tsx's handleDelete for why this has a loading
    // toast but no `error` option, and why the action is awaited
    // separately from the toast.promise call.
    const archiving = (async () => {
      await archiveAccount(id)
      await refresh()
    })()
    toast.promise(archiving, {
      loading: 'Archiving…',
      success: 'Account archived.',
    })
    try {
      await archiving
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  return (
    <section className="flex flex-col gap-4">
      <p className="text-muted-foreground text-sm">
        Where your money lives — bank accounts, cash, cards, and the like.
      </p>

      <form
        onSubmit={handleCreate}
        className="flex max-w-md flex-wrap items-end gap-2"
      >
        <div className="flex flex-1 flex-col gap-1.5">
          <Label htmlFor="account-name">New account name</Label>
          <Input
            id="account-name"
            required
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="account-type">Type</Label>
          <Select
            value={type}
            onValueChange={(value) => setType(value as AccountKind)}
          >
            <SelectTrigger id="account-type">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ACCOUNT_TYPES.map((t) => (
                <SelectItem key={t} value={t}>
                  {accountKindLabel(t)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <Button type="submit" disabled={creating || name === ''}>
          {creating ? 'Adding…' : 'Add'}
        </Button>
      </form>

      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}

      {accounts === null ? (
        <div className="flex items-center gap-2">
          <Spinner />
          <span className="text-muted-foreground text-sm">Loading…</span>
        </div>
      ) : (
        <Card className="[--card-spacing:0]">
          <ul className="divide-border divide-y">
            {accounts
              .filter((a) => !a.archived)
              .map((account) => (
                <li
                  key={account.id}
                  className="flex flex-col gap-2 px-4 py-2.5 text-sm sm:flex-row sm:items-center sm:justify-between"
                >
                  {renamingID === account.id ? (
                    <form
                      className="flex flex-1 flex-wrap items-center gap-2"
                      onSubmit={(event) => {
                        event.preventDefault()
                        handleRename(account.id)
                      }}
                    >
                      <Input
                        autoFocus
                        value={renameValue}
                        onChange={(event) => setRenameValue(event.target.value)}
                      />
                      <Button type="submit" size="sm">
                        Save
                      </Button>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => setRenamingID(null)}
                      >
                        Cancel
                      </Button>
                    </form>
                  ) : (
                    <>
                      <div>
                        <p className="font-medium">{account.name}</p>
                        <p className="text-muted-foreground text-xs">
                          {accountKindLabel(account.type)} · {account.currency}
                        </p>
                      </div>
                      <div className="flex gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => {
                            setRenamingID(account.id)
                            setRenameValue(account.name)
                          }}
                        >
                          Rename
                        </Button>
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => handleArchive(account.id)}
                        >
                          Archive
                        </Button>
                      </div>
                    </>
                  )}
                </li>
              ))}
          </ul>
        </Card>
      )}
    </section>
  )
}
