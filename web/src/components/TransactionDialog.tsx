// Transaction entry and editing (issues #60/#61), unified into one dialog
// instead of a separate /transactions/new page plus TransactionsList's own
// in-row edit form: the same fields, the same validation, reachable from
// anywhere via useTransactionDialog() rather than a route. Editing reuses
// this in full — every field (including notes/tags, which the old in-row
// edit form couldn't show at all and was silently discarding on every
// save) rather than a cut-down subset. Follows ux-principles.md §3's
// fast-entry budget for create (three required fields, everything else
// behind "Add details"); edit shows every field up front, since there's
// no "fast path" for changing something you already recorded.
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { DatePicker } from '@/components/ui/date-picker'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from 'sonner'
import {
  ApiError,
  listAccounts,
  listCategories,
  recordInflow,
  recordOutflow,
  recordTransfer,
  updateTransaction,
  type Account,
  type Category,
  type EditTransactionBody,
  type Transaction,
} from '@/lib/api'
import { sanitizeAmountInput } from '@/lib/utils'
import {
  TransactionDialogContext,
  type SavedEvent,
  type SavedListener,
} from '@/hooks/use-transaction-dialog'

type Kind = 'outflow' | 'inflow' | 'transfer'

const KIND_LABELS: Record<Kind, string> = {
  outflow: 'Spend',
  inflow: 'Receive',
  transfer: 'Move',
}

// Where this dialog remembers the most recently used account, per browser
// (docs/ux-principles.md §3's "account defaults to the most recently
// used, or the only one if there's one") — a per-viewer convenience, not
// data that needs to be shared or durable, so localStorage is the right
// tool rather than a server-side preference.
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

function formatTags(tags: string[] | undefined): string {
  return tags?.join(', ') ?? ''
}

// defaultDescription mirrors the CLI's own description-defaulting decision
// (internal/surface/cli/entries.go's newSpendCmd doc comment, issue #7):
// the application layer requires a non-empty description
// (internal/app/transactions_record.go's resolveCommonFields), but
// ux-principles.md §3's fast-entry budget has no room for a fourth
// required field. The CLI resolves that by having spend/receive's
// category argument do double duty as the description, and move's
// description defaults to "Transfer from <from> to <to>" — this dialog
// follows the identical convention.
function defaultDescription(
  kind: Kind,
  accounts: Account[],
  categories: Category[],
  accountId: string,
  toAccountId: string,
  categoryId: string,
): string {
  if (kind === 'transfer') {
    const fromName = accounts.find((a) => a.id === accountId)?.name ?? ''
    const toName = accounts.find((a) => a.id === toAccountId)?.name ?? ''
    return `Transfer from ${fromName} to ${toName}`
  }
  return categories.find((c) => c.id === categoryId)?.name ?? ''
}

type DialogRequest =
  | { mode: 'create'; onSaved?: (created: Transaction) => void }
  | {
      mode: 'edit'
      transaction: Transaction
      onSaved?: (updated: Transaction) => void
    }

export function TransactionDialogProvider({
  children,
}: {
  children: ReactNode
}) {
  // request isn't cleared on close — only ever replaced by the next open
  // — so Dialog's own close animation has real content to fade out
  // instead of snapping to empty (the same tradeoff Settings.tsx's token
  // dialog already accepts).
  const [request, setRequest] = useState<DialogRequest | null>(null)
  const [open, setOpen] = useState(false)
  const listenersRef = useRef<Set<SavedListener>>(new Set())

  const openCreate = useCallback((onSaved?: (created: Transaction) => void) => {
    setRequest({ mode: 'create', onSaved })
    setOpen(true)
  }, [])

  const openEdit = useCallback(
    (transaction: Transaction, onSaved?: (updated: Transaction) => void) => {
      setRequest({ mode: 'edit', transaction, onSaved })
      setOpen(true)
    },
    [],
  )

  const onTransactionSaved = useCallback((listener: SavedListener) => {
    listenersRef.current.add(listener)
    return () => {
      listenersRef.current.delete(listener)
    }
  }, [])

  const notifySaved = useCallback((event: SavedEvent) => {
    for (const listener of listenersRef.current) listener(event)
  }, [])

  const value = useMemo(
    () => ({ openCreate, openEdit, onTransactionSaved }),
    [openCreate, openEdit, onTransactionSaved],
  )

  return (
    <TransactionDialogContext.Provider value={value}>
      {children}
      {request && (
        <TransactionDialogSheet
          key={request.mode === 'edit' ? request.transaction.id : 'create'}
          request={request}
          open={open}
          onOpenChange={setOpen}
          notifySaved={notifySaved}
        />
      )}
    </TransactionDialogContext.Provider>
  )
}

function TransactionDialogSheet({
  request,
  open,
  onOpenChange,
  notifySaved,
}: {
  request: DialogRequest
  open: boolean
  onOpenChange: (open: boolean) => void
  notifySaved: (event: SavedEvent) => void
}) {
  const editing = request.mode === 'edit' ? request.transaction : null

  const [kind, setKind] = useState<Kind>(editing?.type ?? 'outflow')
  const [accounts, setAccounts] = useState<Account[]>([])
  const [categories, setCategories] = useState<Category[]>([])
  const [loadError, setLoadError] = useState<string | null>(null)

  const [amount, setAmount] = useState(editing?.amount ?? '')
  const [accountId, setAccountId] = useState(
    editing?.account_id ?? editing?.from_account_id ?? '',
  )
  const [toAccountId, setToAccountId] = useState(editing?.to_account_id ?? '')
  const [categoryId, setCategoryId] = useState(editing?.category_id ?? '')

  // Create starts collapsed (ux-principles.md §4's "occasional" layer) —
  // edit shows everything immediately, since there's no fast path for
  // changing something already recorded, and hiding an existing note
  // behind a click would read as if it had been lost.
  const [showDetails, setShowDetails] = useState(editing !== null)
  const [date, setDate] = useState(editing?.date ?? '')
  const [description, setDescription] = useState(editing?.description ?? '')
  const [notes, setNotes] = useState(editing?.notes ?? '')
  const [tags, setTags] = useState(formatTags(editing?.tags))
  const [enterMultiple, setEnterMultiple] = useState(false)

  const [submitError, setSubmitError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [justRecorded, setJustRecorded] = useState(false)

  // Runs on every open, not just this component's first mount. The
  // TransactionDialogProvider's key (request.mode === 'edit' ?
  // transaction.id : 'create') only forces a remount when switching
  // *which* transaction is being edited, or between create and edit — a
  // second openCreate() (or a second openEdit() of the very same
  // transaction) reuses the same Sheet instance with its useState
  // initializers already spent, so without this, every field below
  // would still hold whatever was last typed, and accounts/categories
  // would still hold whatever was fetched on the very first open, missing
  // anything added via Settings since.
  useEffect(() => {
    if (!open) return

    setKind(editing?.type ?? 'outflow')
    setAmount(editing?.amount ?? '')
    const initialAccountId =
      editing?.account_id ?? editing?.from_account_id ?? ''
    const initialToAccountId = editing?.to_account_id ?? ''
    const initialCategoryId = editing?.category_id ?? ''
    setAccountId(initialAccountId)
    setToAccountId(initialToAccountId)
    setCategoryId(initialCategoryId)
    setShowDetails(editing !== null)
    setDate(editing?.date ?? '')
    setDescription(editing?.description ?? '')
    setNotes(editing?.notes ?? '')
    setTags(formatTags(editing?.tags))
    setEnterMultiple(false)
    setSubmitError(null)
    setSubmitting(false)
    setJustRecorded(false)
    setLoadError(null)

    let cancelled = false
    Promise.all([listAccounts(), listCategories()])
      .then(([loadedAccounts, loadedCategories]) => {
        if (cancelled) return
        const active = loadedAccounts.filter(
          (a) =>
            !a.archived ||
            a.id === initialAccountId ||
            a.id === initialToAccountId,
        )
        setAccounts(active)
        setCategories(
          loadedCategories.filter(
            (c) => !c.archived || c.id === initialCategoryId,
          ),
        )
        if (request.mode === 'create') {
          setAccountId((current) => current || defaultAccountId(active))
        }
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
    // Deliberately keyed on `open` alone: `editing`/`request` are read
    // from the closure at the moment `open` flips true, which already
    // reflects that open's own request (setRequest/setOpen are set
    // together in openCreate/openEdit, so both land in the same render).
    // Depending on them too would be redundant and would fight the
    // eslint-exhaustive-deps rule for no benefit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const relevantCategories = useMemo(
    () =>
      categories.filter((c) =>
        kind === 'inflow' ? c.type === 'income' : c.type === 'expense',
      ),
    [categories, kind],
  )

  const hasSingleAccount = accounts.length === 1
  const canSubmit =
    amount.trim() !== '' &&
    accountId !== '' &&
    (kind === 'transfer' ? toAccountId !== '' : categoryId !== '')

  function resetForNextEntry() {
    setAmount('')
    setCategoryId('')
    setToAccountId('')
    setDate('')
    setDescription('')
    setNotes('')
    setTags('')
    setShowDetails(false)
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!canSubmit || submitting) return

    setSubmitError(null)
    setSubmitting(true)
    setJustRecorded(false)
    try {
      if (request.mode === 'edit') {
        const body: EditTransactionBody = {
          amount: amount.trim(),
          description: description.trim(),
          date: date || undefined,
          notes: notes.trim() || undefined,
          tags: parseTags(tags),
          ...(kind === 'transfer'
            ? { from_account: accountId, to_account: toAccountId }
            : { account: accountId, category: categoryId || undefined }),
        }
        const updated = await updateTransaction(request.transaction.id, body)
        request.onSaved?.(updated)
        notifySaved({ mode: 'edit', transaction: updated })
        toast.success('Transaction updated.')
        onOpenChange(false)
        return
      }

      const common = {
        amount: amount.trim(),
        date: date || undefined,
        description:
          description.trim() ||
          defaultDescription(
            kind,
            accounts,
            categories,
            accountId,
            toAccountId,
            categoryId,
          ),
        notes: notes.trim() || undefined,
        tags: parseTags(tags),
      }

      const created =
        kind === 'transfer'
          ? await recordTransfer({
              fromAccount: accountId,
              toAccount: toAccountId,
              ...common,
            })
          : await (kind === 'inflow' ? recordInflow : recordOutflow)({
              account: accountId,
              category: categoryId,
              ...common,
            })

      rememberAccount(accountId)
      request.onSaved?.(created)
      notifySaved({ mode: 'create', transaction: created })

      if (enterMultiple) {
        // Amount, category, and the progressive-disclosure fields reset
        // for the next entry — account and kind stay put: the next
        // transaction is likely the same account, and switching kind
        // between entries is a deliberate choice this shouldn't undo.
        resetForNextEntry()
        setJustRecorded(true)
      } else {
        toast.success('Transaction recorded.')
        onOpenChange(false)
      }
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

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>
            {request.mode === 'edit' ? 'Edit transaction' : 'Add transaction'}
          </DialogTitle>
          {request.mode === 'create' && (
            <DialogDescription>
              Spend, receive, or move money between accounts.
            </DialogDescription>
          )}
        </DialogHeader>

        {loadError ? (
          <p className="text-destructive text-sm" role="alert">
            {loadError}
          </p>
        ) : (
          <form
            className="flex min-h-0 flex-col gap-4"
            onSubmit={handleSubmit}
            noValidate
          >
            <div className="no-scrollbar -mx-4 flex max-h-[70vh] flex-col gap-4 overflow-y-auto px-4">
              {request.mode === 'create' ? (
                <Tabs
                  value={kind}
                  onValueChange={(value) => {
                    setKind(value as Kind)
                    setCategoryId('')
                    setJustRecorded(false)
                  }}
                >
                  <TabsList aria-label="Transaction type" className="w-full">
                    {(Object.keys(KIND_LABELS) as Kind[]).map((k) => (
                      <TabsTrigger key={k} value={k} className="flex-1">
                        {KIND_LABELS[k]}
                      </TabsTrigger>
                    ))}
                  </TabsList>
                </Tabs>
              ) : (
                // The transaction's type isn't editable (EditTransactionBody
                // has no field for it — spend/receive/move are structurally
                // different operations, not just a label) — shown as
                // context, not as a control that looks interactive but
                // isn't.
                <p className="text-muted-foreground text-sm">
                  Editing a {KIND_LABELS[kind].toLowerCase()}
                </p>
              )}

              <div className="flex flex-col gap-1.5">
                <Label htmlFor="td-amount">Amount</Label>
                <Input
                  id="td-amount"
                  inputMode="decimal"
                  placeholder="0.00"
                  autoFocus
                  required
                  value={amount}
                  onChange={(e) =>
                    setAmount(sanitizeAmountInput(e.target.value))
                  }
                />
              </div>

              {kind !== 'transfer' && (
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="td-category">Category</Label>
                  <Select
                    id="td-category"
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
                <Label htmlFor="td-account">
                  {kind === 'transfer' ? 'From account' : 'Account'}
                </Label>
                {hasSingleAccount ? (
                  <p className="text-sm">{accounts[0]?.name}</p>
                ) : (
                  <Select
                    id="td-account"
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
                  <Label htmlFor="td-to-account">To account</Label>
                  <Select
                    id="td-to-account"
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

              {request.mode === 'create' && !showDetails && (
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground cursor-pointer self-start text-sm underline-offset-2 hover:underline"
                  onClick={() => setShowDetails(true)}
                >
                  Add details
                </button>
              )}

              {showDetails && (
                <div className="flex flex-col gap-4 border-t pt-4">
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="td-date">Date</Label>
                    <DatePicker id="td-date" value={date} onChange={setDate} />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="td-description">Description</Label>
                    <Input
                      id="td-description"
                      value={description}
                      onChange={(e) => setDescription(e.target.value)}
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="td-notes">Notes</Label>
                    <Input
                      id="td-notes"
                      value={notes}
                      onChange={(e) => setNotes(e.target.value)}
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="td-tags">Tags</Label>
                    <Input
                      id="td-tags"
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
                <p role="status" className="text-success text-sm">
                  Recorded.
                </p>
              )}
            </div>

            <DialogFooter
              className={
                request.mode === 'create' ? 'sm:justify-between' : undefined
              }
            >
              {request.mode === 'create' && (
                <Label className="order-last flex items-center gap-2 self-center sm:order-first">
                  <Switch
                    checked={enterMultiple}
                    onCheckedChange={setEnterMultiple}
                  />
                  Enter multiple
                </Label>
              )}
              <Button type="submit" disabled={!canSubmit || submitting}>
                {submitting
                  ? 'Saving…'
                  : request.mode === 'edit'
                    ? 'Save'
                    : `Record ${KIND_LABELS[kind].toLowerCase()}`}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
