// The real settings screen (issue #62), replacing routes.tsx's
// placeholder. Three sections, each calling exactly one existing (or
// #62-added) application method via web/src/lib/settings.ts — no
// business logic lives here, only presentation and request/response
// wiring (docs/architecture.md §3).
import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError } from '@/lib/api'
import {
  archiveAccount,
  archiveCategory,
  changePassword,
  createAccount,
  createApiToken,
  createCategory,
  listAccounts,
  listApiTokens,
  listCategories,
  renameAccount,
  renameCategory,
  reparentCategory,
  revokeApiToken,
  type Account,
  type ApiToken,
  type Category,
  type CreatedApiToken,
} from '@/lib/settings'

function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : 'Couldn’t reach the server. Try again.'
}

function Section({
  title,
  description,
  children,
}: {
  title: string
  description: string
  children: React.ReactNode
}) {
  return (
    <section className="border-border flex flex-col gap-4 border-b pb-8 last:border-b-0">
      <div>
        <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
        <p className="text-muted-foreground text-sm">{description}</p>
      </div>
      {children}
    </section>
  )
}

function PasswordSection() {
  const navigate = useNavigate()
  const [newPassword, setNewPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    if (newPassword !== confirm) {
      setError('Passwords didn’t match.')
      return
    }
    setSubmitting(true)
    try {
      await changePassword(newPassword)
      // changePassword revokes every session, including this one
      // (internal/surface/http/auth.go's changePassword doc comment) -
      // the browser's cookie is already dead, so head to login rather
      // than staying on a page that will 401 on its next request.
      navigate('/login', { replace: true })
    } catch (err) {
      setError(errorMessage(err))
      setSubmitting(false)
    }
  }

  return (
    <Section
      title="Password"
      description="Changing your password signs you out everywhere, including this browser."
    >
      <form
        className="flex max-w-xs flex-col gap-4"
        onSubmit={handleSubmit}
        noValidate
      >
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="new-password">New password</Label>
          <Input
            id="new-password"
            type="password"
            autoComplete="new-password"
            required
            value={newPassword}
            onChange={(event) => setNewPassword(event.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="confirm-password">Confirm new password</Label>
          <Input
            id="confirm-password"
            type="password"
            autoComplete="new-password"
            required
            aria-invalid={error !== null}
            value={confirm}
            onChange={(event) => setConfirm(event.target.value)}
          />
        </div>
        {error && (
          <p role="alert" className="text-destructive text-sm">
            {error}
          </p>
        )}
        <Button
          type="submit"
          disabled={submitting || newPassword === '' || confirm === ''}
          className="self-start"
        >
          {submitting ? 'Changing…' : 'Change password'}
        </Button>
      </form>
    </Section>
  )
}

function ApiTokensSection() {
  const [tokens, setTokens] = useState<ApiToken[] | null>(null)
  const [name, setName] = useState('')
  const [creating, setCreating] = useState(false)
  const [justCreated, setJustCreated] = useState<CreatedApiToken | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      setTokens(await listApiTokens())
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
      const created = await createApiToken(name)
      setJustCreated(created)
      setName('')
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setCreating(false)
    }
  }

  async function handleRevoke(id: string) {
    setError(null)
    try {
      await revokeApiToken(id)
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  return (
    <Section
      title="API tokens"
      description="Tokens authenticate scripts and other external clients. A token's value is shown once, when it's created."
    >
      {justCreated && (
        <div className="border-border bg-muted flex flex-col gap-1 rounded-lg border p-3 text-sm">
          <p>
            Created <strong>{justCreated.name}</strong>. Copy this token now —
            it can’t be shown again.
          </p>
          <code className="bg-background rounded border px-2 py-1 break-all">
            {justCreated.token}
          </code>
          <Button
            variant="ghost"
            size="sm"
            className="self-start"
            onClick={() => setJustCreated(null)}
          >
            Dismiss
          </Button>
        </div>
      )}

      <form onSubmit={handleCreate} className="flex max-w-sm items-end gap-2">
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

      {error && (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      )}

      {tokens === null ? (
        <p className="text-muted-foreground text-sm">Loading…</p>
      ) : tokens.length === 0 ? (
        <p className="text-muted-foreground text-sm">No API tokens yet.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {tokens.map((token) => (
            <li
              key={token.id}
              className="border-border flex items-center justify-between rounded-lg border px-3 py-2 text-sm"
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
      )}
    </Section>
  )
}

const ACCOUNT_TYPES = [
  'bank',
  'cash',
  'credit_card',
  'wallet',
  'investment',
  'loan',
  'other',
]

function AccountsSection() {
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
    try {
      await archiveAccount(id)
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  return (
    <Section
      title="Accounts"
      description="Where your money lives — bank accounts, cash, cards, and the like."
    >
      <form onSubmit={handleCreate} className="flex max-w-md items-end gap-2">
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
          <select
            id="account-type"
            className="border-input bg-background h-8 rounded-lg border px-2.5 text-sm shadow-xs"
            value={type}
            onChange={(event) => setType(event.target.value)}
          >
            {ACCOUNT_TYPES.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
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
        <p className="text-muted-foreground text-sm">Loading…</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {accounts
            .filter((a) => !a.archived)
            .map((account) => (
              <li
                key={account.id}
                className="border-border flex items-center justify-between rounded-lg border px-3 py-2 text-sm"
              >
                {renamingID === account.id ? (
                  <form
                    className="flex flex-1 items-center gap-2"
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
                        {account.type} · {account.currency}
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
      )}
    </Section>
  )
}

const CATEGORY_TYPES = ['expense', 'income']

function CategoriesSection() {
  const [categories, setCategories] = useState<Category[] | null>(null)
  const [name, setName] = useState('')
  const [type, setType] = useState(CATEGORY_TYPES[0])
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [renamingID, setRenamingID] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState('')

  async function refresh() {
    try {
      setCategories(await listCategories())
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
      await createCategory({ name, type })
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
      await renameCategory(id, renameValue)
      setRenamingID(null)
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  async function handleArchive(id: string) {
    setError(null)
    try {
      await archiveCategory(id)
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  async function handleReparent(id: string, parent: string) {
    setError(null)
    try {
      await reparentCategory(id, parent)
      await refresh()
    } catch (err) {
      setError(errorMessage(err))
    }
  }

  return (
    <Section
      title="Categories"
      description="How your income and expenses are grouped."
    >
      <form onSubmit={handleCreate} className="flex max-w-md items-end gap-2">
        <div className="flex flex-1 flex-col gap-1.5">
          <Label htmlFor="category-name">New category name</Label>
          <Input
            id="category-name"
            required
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="category-type">Type</Label>
          <select
            id="category-type"
            className="border-input bg-background h-8 rounded-lg border px-2.5 text-sm shadow-xs"
            value={type}
            onChange={(event) => setType(event.target.value)}
          >
            {CATEGORY_TYPES.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
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

      {categories === null ? (
        <p className="text-muted-foreground text-sm">Loading…</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {categories
            .filter((c) => !c.archived)
            .map((category) => (
              <li
                key={category.id}
                className="border-border flex items-center justify-between rounded-lg border px-3 py-2 text-sm"
              >
                {renamingID === category.id ? (
                  <form
                    className="flex flex-1 items-center gap-2"
                    onSubmit={(event) => {
                      event.preventDefault()
                      handleRename(category.id)
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
                      <p className="font-medium">{category.name}</p>
                      <p className="text-muted-foreground text-xs">
                        {category.type}
                      </p>
                    </div>
                    <div className="flex items-center gap-2">
                      <Label
                        htmlFor={`parent-${category.id}`}
                        className="text-muted-foreground text-xs font-normal"
                      >
                        Parent
                      </Label>
                      <select
                        id={`parent-${category.id}`}
                        className="border-input bg-background h-7 rounded-lg border px-2 text-xs"
                        value={category.parent_id ?? ''}
                        onChange={(event) =>
                          handleReparent(category.id, event.target.value)
                        }
                      >
                        <option value="">Top level</option>
                        {(categories ?? [])
                          .filter((c) => c.id !== category.id)
                          .map((c) => (
                            <option key={c.id} value={c.id}>
                              {c.name}
                            </option>
                          ))}
                      </select>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => {
                          setRenamingID(category.id)
                          setRenameValue(category.name)
                        }}
                      >
                        Rename
                      </Button>
                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={() => handleArchive(category.id)}
                      >
                        Archive
                      </Button>
                    </div>
                  </>
                )}
              </li>
            ))}
        </ul>
      )}
    </Section>
  )
}

export function Settings() {
  return (
    <div className="flex flex-1 flex-col gap-8 p-6">
      <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
      <PasswordSection />
      <ApiTokensSection />
      <AccountsSection />
      <CategoriesSection />
    </div>
  )
}
