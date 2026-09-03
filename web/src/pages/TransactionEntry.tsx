// Fast transaction entry (issue #60): spend / receive / move, the CLI's
// own verbs (docs/ux-principles.md §7 — "one vocabulary, not two"). Follows
// §3's fast-entry budget: three required fields for the common case
// (amount, category, account), a server-resolved date the user never has
// to type for "today", account defaulting to the most recently used, one
// screen with no confirmation dialog, and everything else (date, notes,
// tags) one deliberate step away behind "Add details" (§4's "occasional"
// layer). Amount is passed through to the API as a raw string — this
// component never parses or validates its format (architecture.md §3).
import { useEffect, useMemo, useState, type FormEvent } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import {
  ApiError,
  listAccounts,
  listCategories,
  recordInflow,
  recordOutflow,
  recordTransfer,
  type Account,
  type Category,
} from '@/lib/api'

type Kind = 'outflow' | 'inflow' | 'transfer'

const KIND_LABELS: Record<Kind, string> = {
  outflow: 'Spend',
  inflow: 'Receive',
  transfer: 'Move',
}

// LAST_ACCOUNT_KEY is where this screen remembers the most recently used
// account, per browser (docs/ux-principles.md §3's "account defaults to
// the most recently used, or the only one if there's one") — a per-viewer
// convenience, not data that needs to be shared or durable, so
// localStorage is the right tool rather than a server-side preference.
const LAST_ACCOUNT_KEY = 'bodger:lastAccountId'

function rememberAccount(id: string) {
  try {
    localStorage.setItem(LAST_ACCOUNT_KEY, id)
  } catch {
    // A private window or blocked site data throws here; losing the
    // recency default isn't worth failing the transaction over.
  }
}

function lastUsedAccount(): string | null {
  try {
    return localStorage.getItem(LAST_ACCOUNT_KEY)
  } catch {
    return null
  }
}

// defaultAccountId picks the account the form should preselect: the
// remembered last-used one if it's still a real, unarchived account,
// otherwise the only account if there's exactly one, otherwise the first
// in the list — never a forced empty selection when a reasonable default
// exists.
function defaultAccountId(accounts: Account[]): string {
  const remembered = lastUsedAccount()
  if (remembered && accounts.some((a) => a.id === remembered)) {
    return remembered
  }
  return accounts[0]?.id ?? ''
}

function parseTags(raw: string): string[] | undefined {
  const tags = raw
    .split(',')
    .map((t) => t.trim())
    .filter((t) => t !== '')
  return tags.length > 0 ? tags : undefined
}

export function TransactionEntry() {
  const [kind, setKind] = useState<Kind>('outflow')
  const [accounts, setAccounts] = useState<Account[]>([])
  const [categories, setCategories] = useState<Category[]>([])
  const [loadError, setLoadError] = useState<string | null>(null)

  const [amount, setAmount] = useState('')
  const [accountId, setAccountId] = useState('')
  const [toAccountId, setToAccountId] = useState('')
  const [categoryId, setCategoryId] = useState('')

  const [showDetails, setShowDetails] = useState(false)
  const [date, setDate] = useState('')
  const [description, setDescription] = useState('')
  const [notes, setNotes] = useState('')
  const [tags, setTags] = useState('')

  const [submitError, setSubmitError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [justRecorded, setJustRecorded] = useState(false)

  useEffect(() => {
    let cancelled = false
    Promise.all([listAccounts(), listCategories()])
      .then(([loadedAccounts, loadedCategories]) => {
        if (cancelled) return
        const active = loadedAccounts.filter((a) => !a.archived)
        setAccounts(active)
        setCategories(loadedCategories.filter((c) => !c.archived))
        setAccountId((current) => current || defaultAccountId(active))
      })
      .catch((err) => {
        if (cancelled) return
        setLoadError(
          err instanceof ApiError
            ? err.message
            : 'Couldn’t load your accounts and categories. Try again in a moment.',
        )
      })
    return () => {
      cancelled = true
    }
  }, [])

  const relevantCategories = useMemo(
    () =>
      categories.filter((c) =>
        kind === 'inflow' ? c.type === 'income' : c.type === 'expense',
      ),
    [categories, kind],
  )

  // Switching kind can leave a category selected that no longer matches
  // the new kind's list (an expense category while switching to Receive,
  // say) — clear it rather than silently submitting a mismatched one.
  function handleKindChange(next: Kind) {
    setKind(next)
    setCategoryId('')
    setJustRecorded(false)
  }

  const hasSingleAccount = accounts.length === 1
  const canSubmit =
    amount.trim() !== '' &&
    accountId !== '' &&
    (kind === 'transfer' ? toAccountId !== '' : categoryId !== '')

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canSubmit || submitting) return

    setSubmitError(null)
    setSubmitting(true)
    setJustRecorded(false)
    try {
      const common = {
        amount: amount.trim(),
        date: date || undefined,
        description: description.trim() || undefined,
        notes: notes.trim() || undefined,
        tags: parseTags(tags),
      }

      if (kind === 'transfer') {
        await recordTransfer({
          fromAccount: accountId,
          toAccount: toAccountId,
          ...common,
        })
      } else {
        const record = kind === 'inflow' ? recordInflow : recordOutflow
        await record({ account: accountId, category: categoryId, ...common })
      }

      rememberAccount(accountId)
      // Amount, category, and the progressive-disclosure fields reset for
      // the next entry — partial input from a *failed* submit is kept
      // instead (below), per ux-principles.md §5 ("partial input is
      // preserved"), but a successful one is exactly the point where
      // starting fresh is what's wanted. Account and kind stay put: the
      // next transaction is likely the same account, and switching kind
      // between entries is a deliberate choice this shouldn't undo.
      setAmount('')
      setCategoryId('')
      setToAccountId('')
      setDate('')
      setDescription('')
      setNotes('')
      setTags('')
      setShowDetails(false)
      setJustRecorded(true)
    } catch (err) {
      setSubmitError(
        err instanceof ApiError
          ? err.message
          : 'Couldn’t reach the server. Try again.',
      )
    } finally {
      setSubmitting(false)
    }
  }

  if (loadError) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 p-12 text-center">
        <p className="text-destructive text-sm" role="alert">
          {loadError}
        </p>
      </div>
    )
  }

  return (
    <div className="flex flex-1 flex-col items-center p-6">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div
          className="flex gap-1"
          role="radiogroup"
          aria-label="Transaction type"
        >
          {(Object.keys(KIND_LABELS) as Kind[]).map((k) => (
            <Button
              key={k}
              type="button"
              size="sm"
              variant={kind === k ? 'default' : 'outline'}
              aria-pressed={kind === k}
              className="flex-1"
              onClick={() => handleKindChange(k)}
            >
              {KIND_LABELS[k]}
            </Button>
          ))}
        </div>

        <form
          className="flex flex-col gap-4"
          onSubmit={handleSubmit}
          noValidate
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="amount">Amount</Label>
            <Input
              id="amount"
              name="amount"
              inputMode="decimal"
              placeholder="0.00"
              autoFocus
              required
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
            />
          </div>

          {kind !== 'transfer' && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="category">Category</Label>
              <Select
                id="category"
                required
                value={categoryId}
                onChange={(e) => setCategoryId(e.target.value)}
              >
                <option value="" disabled>
                  Choose a category
                </option>
                {relevantCategories.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </Select>
            </div>
          )}

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="account">
              {kind === 'transfer' ? 'From account' : 'Account'}
            </Label>
            {hasSingleAccount ? (
              <p className="text-sm">{accounts[0]?.name}</p>
            ) : (
              <Select
                id="account"
                required
                value={accountId}
                onChange={(e) => setAccountId(e.target.value)}
              >
                <option value="" disabled>
                  Choose an account
                </option>
                {accounts.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </Select>
            )}
          </div>

          {kind === 'transfer' && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="to-account">To account</Label>
              <Select
                id="to-account"
                required
                value={toAccountId}
                onChange={(e) => setToAccountId(e.target.value)}
              >
                <option value="" disabled>
                  Choose an account
                </option>
                {accounts
                  .filter((a) => a.id !== accountId)
                  .map((a) => (
                    <option key={a.id} value={a.id}>
                      {a.name}
                    </option>
                  ))}
              </Select>
            </div>
          )}

          {!showDetails && (
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground self-start text-sm underline-offset-2 hover:underline"
              onClick={() => setShowDetails(true)}
            >
              Add details
            </button>
          )}

          {showDetails && (
            <div className="flex flex-col gap-4 border-t pt-4">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="date">Date</Label>
                <Input
                  id="date"
                  name="date"
                  type="date"
                  value={date}
                  onChange={(e) => setDate(e.target.value)}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="description">Description</Label>
                <Input
                  id="description"
                  name="description"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="notes">Notes</Label>
                <Input
                  id="notes"
                  name="notes"
                  value={notes}
                  onChange={(e) => setNotes(e.target.value)}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="tags">Tags</Label>
                <Input
                  id="tags"
                  name="tags"
                  placeholder="comma, separated"
                  value={tags}
                  onChange={(e) => setTags(e.target.value)}
                />
              </div>
            </div>
          )}

          {submitError && (
            <p role="alert" className="text-destructive text-sm">
              {submitError}
            </p>
          )}
          {justRecorded && !submitError && (
            <p
              role="status"
              className="text-sm text-green-700 dark:text-green-500"
            >
              Recorded.
            </p>
          )}

          <Button type="submit" disabled={!canSubmit || submitting}>
            {submitting
              ? 'Recording…'
              : `Record ${KIND_LABELS[kind].toLowerCase()}`}
          </Button>
        </form>
      </div>
    </div>
  )
}
