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
import { Combobox, type ComboboxOption } from '@/components/ui/combobox'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from 'sonner'
import {
  ApiError,
  getReportingCurrency,
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
import { buildCategoryTree } from '@/lib/category-tree'
import { createCategory } from '@/lib/settings'
import { sanitizeAmountInput } from '@/lib/utils'
import { FxConversionHint } from '@/components/FxConversionHint'
import { useFxConversionHint } from '@/hooks/use-fx-conversion-hint'
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
  // '' means "not loaded yet" (or the actor has no reporting currency
  // configured and the fetch itself failed) — the foreign-currency hint
  // below only renders once this is a real currency code, so a
  // single-currency user or a still-loading dialog never briefly flashes
  // a hint before this settles (issue #138's "no behavior change for a
  // single-currency user").
  const [reportingCurrency, setReportingCurrency] = useState('')
  const [loadError, setLoadError] = useState<string | null>(null)

  const [amount, setAmount] = useState(editing?.amount ?? '')
  const [accountId, setAccountId] = useState(
    editing?.account_id ?? editing?.from_account_id ?? '',
  )
  const [toAccountId, setToAccountId] = useState(editing?.to_account_id ?? '')
  const [categoryId, setCategoryId] = useState(editing?.category_id ?? '')

  // The destination leg's own amount on a cross-currency move (issue
  // #138/#159/#163). On edit, pre-filled from the transaction being
  // edited: transactionView now reports a transfer's to-leg distinctly as
  // to_amount (dto.go's transactionView doc comment, issue #163's fix —
  // before that, a transfer's GET representation had no way to state the
  // to-leg's own amount at all, so this always started empty). On create
  // there is no prior transfer to read one from, so this still starts
  // empty and is filled only by the rate-suggestion effect below.
  // Omitting this on submit falls back to exactly today's behavior
  // (the from-leg's raw digits, reinterpreted in the to-currency).
  const [toAmount, setToAmount] = useState(editing?.to_amount ?? '')
  // Tracks whether toAmount already holds real, user-authoritative data —
  // either typed by the user, or (on edit) read back from the transaction
  // itself — so the rate-suggestion effect below (over in the render
  // section) stops overwriting it. The pre-fill from a fetched rate is
  // only ever a suggestion, never a value real data gets silently
  // replaced by.
  const [toAmountTouched, setToAmountTouched] = useState(
    Boolean(editing?.to_amount),
  )
  // The exact from/to account pair `editing` itself came in with, when
  // it's a transfer — used only to keep the toAmount pre-fill above from
  // being wiped by the "different pair means start over" effect a little
  // further down, which would otherwise also fire on this component's
  // very first render (an effect's dependency array skips *re-runs* for
  // an unchanged pair, not the initial run — there is no "previous pair"
  // to compare against on mount).
  const editTransferPairRef = useRef<{ from: string; to: string } | null>(
    editing?.type === 'transfer'
      ? { from: editing.from_account_id ?? '', to: editing.to_account_id ?? '' }
      : null,
  )

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
    setToAmount(editing?.to_amount ?? '')
    setToAmountTouched(Boolean(editing?.to_amount))
    editTransferPairRef.current =
      editing?.type === 'transfer'
        ? { from: initialAccountId, to: initialToAccountId }
        : null
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
    setReportingCurrency('')

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
    // Fetched independently of accounts/categories, and never surfaced as
    // a blocking loadError: the foreign-currency hint is a nice-to-have
    // on top of entry, not something that should stop a user from
    // recording a transaction if it fails to load.
    getReportingCurrency()
      .then((rc) => {
        if (cancelled) return
        setReportingCurrency(rc.currency)
      })
      .catch(() => {
        // Left as '' — every hint below stays hidden, same as a
        // single-currency user (issue #138's "no behavior change"
        // requirement degrades safely here too).
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

  // Issue #106: the same parent_id tree Settings' own category list and
  // "Parent" field show, flattened into the Combobox's option shape.
  // Built from relevantCategories (not the raw `categories` state) so a
  // category tree already reflects the current Spend/Receive tab's own
  // kind filter — an expense category never shows up while entering
  // income, matching what the old <select> already did.
  const categoryOptions = useMemo<ComboboxOption[]>(
    () =>
      buildCategoryTree(relevantCategories).map(
        ({ category, depth, path }) => ({
          value: category.id,
          label: category.name,
          depth,
          path,
        }),
      ),
    [relevantCategories],
  )

  // Quick-create (issue #106): called from the Category combobox itself
  // when the typed text matches nothing. Always creates a new top-level
  // category of whichever kind the dialog is currently in (Spend →
  // expense, Receive → income) — nesting it under a specific parent from
  // here would need its own picker inside the create row, which is more
  // than "don't abandon the transaction" calls for; a quick-created
  // category can always be reparented later from Settings, same as any
  // other. Errors surface as a toast rather than inline: there's no
  // dedicated error slot inside the combobox's create row, and a toast
  // doesn't block the transaction still mid-entry underneath it.
  async function handleCreateCategory(name: string): Promise<ComboboxOption> {
    try {
      const created = await createCategory({
        name,
        type: kind === 'inflow' ? 'income' : 'expense',
      })
      setCategories((prev) => [...prev, created])
      return { value: created.id, label: created.name, path: created.name }
    } catch (err) {
      toast.error(
        err instanceof ApiError
          ? err.message
          : 'Couldn’t create that category. Try again.',
      )
      throw err
    }
  }

  const hasSingleAccount = accounts.length === 1
  const canSubmit =
    amount.trim() !== '' &&
    accountId !== '' &&
    (kind === 'transfer' ? toAccountId !== '' : categoryId !== '')

  // The foreign-currency hint (issue #138): amount is always entered in
  // the primary account's own currency (accountId — the "from" account
  // for a move, the only account otherwise), so that's what a non-empty,
  // non-reporting-currency amount is converted from. Never enabled for a
  // single-currency user, since reportingCurrency then resolves to the
  // same currency every account already uses.
  const primaryCurrency =
    accounts.find((a) => a.id === accountId)?.currency ?? ''
  const conversionHint = useFxConversionHint({
    from: primaryCurrency,
    to: reportingCurrency,
    amount,
    date,
  })

  // The destination-leg amount field (issue #138's first bullet, unblocked
  // by #159's to_amount support): only shown for a transfer whose two
  // accounts actually differ in currency. toCurrency here is the
  // *to*-account's own currency, which is not necessarily the reporting
  // currency — the hook's own refresh fetches this exact pair directly
  // (issue #165), not just pairs quoted against the reporting currency.
  // Until a rate is actually fetched, this degrades to the same "no
  // stored rate yet" state the read-only hint already handles — the
  // field itself stays fully editable regardless, since a suggestion is
  // never required to submit.
  const toCurrency = accounts.find((a) => a.id === toAccountId)?.currency ?? ''
  const crossCurrencyTransfer =
    kind === 'transfer' &&
    primaryCurrency !== '' &&
    toCurrency !== '' &&
    primaryCurrency !== toCurrency
  const toAmountHint = useFxConversionHint({
    from: primaryCurrency,
    to: toCurrency,
    amount,
    date,
  })

  // Suggests toAmount from the fetched rate whenever one becomes
  // available — but only until the user types their own value (per
  // toAmountTouched), since the pre-fill is a convenience, never
  // authoritative over what the user actually enters.
  useEffect(() => {
    if (!crossCurrencyTransfer || toAmountTouched) return
    if (toAmountHint.state.status !== 'ready') return
    // converted is always set here — this hook always calls getFxRate
    // with `amount` (useFxConversionHint's own `enabled` check requires a
    // non-empty amount), and the API only omits it when amount was never
    // given.
    const converted = toAmountHint.state.rate.converted
    if (!converted) return
    const [suggested] = converted.split(' ')
    setToAmount(suggested)
  }, [crossCurrencyTransfer, toAmountTouched, toAmountHint.state])

  // A different pair of accounts means any prior suggestion or manual
  // entry no longer means anything for this one — except the exact pair
  // the transfer being edited already had, whose toAmount is real data
  // read back from the transaction (set above, and re-set by the "on
  // open" effect), not a stale suggestion this effect should clear. That
  // includes this effect's own first run on mount, when there is no
  // "previous" pair to compare against.
  useEffect(() => {
    const original = editTransferPairRef.current
    if (
      original &&
      original.from === accountId &&
      original.to === toAccountId
    ) {
      return
    }
    setToAmount('')
    setToAmountTouched(false)
  }, [accountId, toAccountId])

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
            ? {
                from_account: accountId,
                to_account: toAccountId,
                to_amount: toAmount.trim() || undefined,
              }
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
              toAmount: toAmount.trim() || undefined,
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
                {reportingCurrency !== '' && (
                  <FxConversionHint hint={conversionHint} />
                )}
              </div>

              {kind !== 'transfer' && (
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="td-category">Category</Label>
                  <Combobox
                    id="td-category"
                    options={categoryOptions}
                    value={categoryId}
                    onChange={setCategoryId}
                    placeholder="Choose a category"
                    searchPlaceholder="Search categories…"
                    emptyText="No matching categories."
                    onCreate={handleCreateCategory}
                  />
                </div>
              )}

              <div className="flex flex-col gap-1.5">
                <Label htmlFor="td-account">
                  {kind === 'transfer' ? 'From account' : 'Account'}
                </Label>
                {hasSingleAccount ? (
                  <p className="text-sm">{accounts[0]?.name}</p>
                ) : accounts.length === 0 ? (
                  <p className="text-muted-foreground text-sm">
                    Loading accounts…
                  </p>
                ) : (
                  // Rendered only once `accounts` has actually loaded: Radix
                  // Select renders a hidden native <select> mirror whenever
                  // its trigger sits inside a real <form> (true here,
                  // regardless of whether a `name` prop is passed), keyed
                  // to the set of option values. Mounting this Select
                  // first with zero options and letting `accounts` arrive
                  // afterward changes that key mid-lifecycle, which forces
                  // Radix to destroy and recreate the native mirror and,
                  // in the process, dispatch a synthetic change event that
                  // silently resets the controlled value back to "" —
                  // confirmed by direct reproduction against
                  // @radix-ui/react-select's own SelectBubbleInput source,
                  // not a jsdom-only artifact. Mounting once the real
                  // option list is already final avoids the mid-lifecycle
                  // key change entirely.
                  <Select
                    required
                    value={accountId}
                    onValueChange={(value) => {
                      setAccountId(value)
                      // A stale "to account" that now equals the new
                      // "from account" is invalid (can't transfer to
                      // yourself) — clear it rather than silently
                      // submitting a transfer to/from the same account.
                      setToAccountId((current) =>
                        current === value ? '' : current,
                      )
                    }}
                  >
                    <SelectTrigger id="td-account">
                      <SelectValue placeholder="Choose an account" />
                    </SelectTrigger>
                    <SelectContent>
                      {accounts.map((a) => (
                        <SelectItem key={a.id} value={a.id}>
                          {a.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              </div>

              {kind === 'transfer' && (
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="td-to-account">To account</Label>
                  {accounts.length === 0 ? (
                    <p className="text-muted-foreground text-sm">
                      Loading accounts…
                    </p>
                  ) : (
                    <Select
                      // Keyed on the excluded account: this option list
                      // changes shape every time `accountId` does (see the
                      // "From account" comment above for why that alone
                      // is enough to spuriously reset a Radix Select's
                      // value through its native form-mirror). A fresh
                      // key forces a clean remount with the final option
                      // list already in place, rather than letting Radix
                      // mutate an existing instance's options out from
                      // under it.
                      key={accountId}
                      required
                      value={toAccountId}
                      onValueChange={setToAccountId}
                    >
                      <SelectTrigger id="td-to-account">
                        <SelectValue placeholder="Choose an account" />
                      </SelectTrigger>
                      <SelectContent>
                        {accounts
                          .filter((a) => a.id !== accountId)
                          .map((a) => (
                            <SelectItem key={a.id} value={a.id}>
                              {a.name}
                            </SelectItem>
                          ))}
                      </SelectContent>
                    </Select>
                  )}
                </div>
              )}

              {crossCurrencyTransfer && (
                <div className="flex flex-col gap-1.5">
                  <Label htmlFor="td-to-amount">
                    Amount received ({toCurrency})
                  </Label>
                  <Input
                    id="td-to-amount"
                    inputMode="decimal"
                    placeholder="0.00"
                    value={toAmount}
                    onChange={(e) => {
                      setToAmountTouched(true)
                      setToAmount(sanitizeAmountInput(e.target.value))
                    }}
                  />
                  <FxConversionHint hint={toAmountHint} />
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
