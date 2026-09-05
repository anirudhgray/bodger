// apiFetch is the one place the web UI calls bodger's REST API from
// (issue #59) — every surface normalises its own transport concerns once
// rather than each call site reimplementing them (CLAUDE.md's
// normalise-once rule): it always sends the browser's session cookie
// (credentials: "include"), and it always attaches the X-Bodger-CSRF
// header on a state-changing request, since every cookie-authenticated
// POST/PUT/PATCH/DELETE needs it or the API rejects the request outright
// (internal/surface/http/auth_middleware.go, ADR-0006). A bearer-token
// caller wouldn't need this, but the web UI is cookie-only by design (no
// client-side token handling), so there's no case where it doesn't apply.
import type { components } from './api-types'

const CSRF_HEADER = 'X-Bodger-CSRF'
const SAFE_METHODS = new Set(['GET', 'HEAD'])

// ApiError is what apiFetch throws for any non-2xx response — the wire
// shape internal/surface/http/respond.go's errorEnvelope defines, minus
// the "error" wrapper key. code and field mirror errs.Error's own
// registry (internal/platform/errs) closely enough to branch on
// (e.g. code === "invalid_input") without hardcoding HTTP status numbers
// here.
export class ApiError extends Error {
  code: string
  field?: string

  constructor(code: string, message: string, field?: string) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.field = field
  }
}

type ErrorBody = {
  error?: { code?: string; message?: string; field?: string }
}

type DataBody<T> = { data: T }

// apiFetch calls path against bodger's REST API (proxied to a local
// `bodger serve` in dev — web/vite.config.ts) and unwraps the {"data":
// ...} envelope every successful response uses. init.body, if present, is
// assumed to already be a JSON string (callers pass JSON.stringify(...)
// themselves, matching how few and simple the request bodies here are).
export async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const method = (init.method ?? 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  if (!SAFE_METHODS.has(method)) {
    headers.set(CSRF_HEADER, '1')
  }
  if (init.body !== undefined && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const res = await fetch(path, {
    ...init,
    method,
    headers,
    credentials: 'include',
  })

  let body: unknown
  try {
    body = await res.json()
  } catch {
    body = undefined
  }

  if (!res.ok) {
    const errBody = (body as ErrorBody | undefined)?.error
    throw new ApiError(
      errBody?.code ?? 'internal',
      errBody?.message ?? 'Something went wrong. Try again in a moment.',
      errBody?.field,
    )
  }

  return (body as DataBody<T>).data
}

// BalanceEntry is one account's computed balance, as GET /api/v1/balances
// (internal/surface/http/balances.go) returns it: amount is a plain
// decimal string in currency's minor-unit precision, not a number — never
// parsed as a float here, only ever displayed as-is (issue #63; no
// client-side financial logic, docs/architecture.md §3). Aliased (not
// re-spelled) from the generated schema (issue #83) — see this file's
// Account/Category types below for why.
export type BalanceEntry = components['schemas']['Balance']

export type Balances = components['schemas']['Balances']

// getBalances fetches every account's balance as of today (the API
// resolves "today" server-side when as_of is omitted — normalise-once,
// ADR-0005 — this call never passes one).
export function getBalances(): Promise<Balances> {
  return apiFetch<Balances>('/api/v1/balances')
}

// --- Transactions (issue #61) ---------------------------------------------
//
// Typed helpers over apiFetch for the transaction list screen. The wire
// vocabulary here is "outflow"/"inflow"/"transfer" — internal/surface/http's
// own spelling (internal/surface/http/router.go's "type" query parameter
// and internal/surface/http/dto.go's transactionView) — not the CLI's
// spend/receive/move verbs, which are a deliberate CLI-only divergence
// (internal/surface/cli/transactions.go's own doc comment on
// transactionKindFor explains why). The page component, not this file,
// is where that gets translated for display.

// Transaction mirrors internal/surface/http/dto.go's transactionView:
// account_id/category_id are set for an outflow or inflow,
// from_account_id/to_account_id for a transfer — never both pairs at once.
// Generated from openapi.json (issue #83), not hand-transcribed — see
// this file's Account/Category types below for why that matters.
export type Transaction = components['schemas']['Transaction']

export type TransactionKind = Transaction['type']

export type TransactionListFilter = {
  account?: string
  category?: string
  type?: TransactionKind
  from?: string
  to?: string
  cursor?: string
}

export type TransactionListPage = components['schemas']['TransactionList']

function buildQuery(params: Record<string, string | undefined>): string {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value) query.set(key, value)
  }
  const s = query.toString()
  return s ? `?${s}` : ''
}

export function listTransactions(
  filter: TransactionListFilter = {},
): Promise<TransactionListPage> {
  return apiFetch<TransactionListPage>(
    `/api/v1/transactions${buildQuery(filter)}`,
  )
}

// EditTransactionBody is a full replacement of the transaction's editable
// fields (internal/surface/http/auth.go's sibling, editTransactionRequest,
// documents why: there's no way to say "leave this field alone" in a
// command whose string fields already use "" to mean something else) — a
// caller must send the transaction's complete new state, not a partial
// patch.
export type EditTransactionBody = {
  account?: string
  category?: string
  from_account?: string
  to_account?: string
  currency?: string
  amount: string
  date?: string
  description: string
  notes?: string
  tags?: string[]
}

export function updateTransaction(
  id: string,
  body: EditTransactionBody,
): Promise<Transaction> {
  return apiFetch<Transaction>(`/api/v1/transactions/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
}

export function deleteTransaction(id: string): Promise<Transaction> {
  return apiFetch<Transaction>(`/api/v1/transactions/${id}`, {
    method: 'DELETE',
  })
}

// --- Accounts and categories (shared reference data) ----------------------
// Three sibling issues (#60, #61, #62) each independently needed these
// lookups, and each hand-transcribed its own guess of the wire shape —
// two of the three guessed plausible-sounding but nonexistent account
// kinds ("checking", "savings") instead of the real set. issue #83 closed
// that gap: Account/Category (and their "type" enums) are now aliased
// straight from ./api-types.ts, which `npm run generate:api-types`
// regenerates from internal/surface/http/openapi.json — itself generated
// from internal/surface/http/dto.go's accountView/categoryView and
// openapi_gen.go's enum table (see docs/contributing.md's "Regenerating
// the frontend's API types" section). A field or enum value that doesn't
// actually exist on the wire is now a tsc error here, not a silent guess.

export type Account = components['schemas']['Account']
export type AccountKind = Account['type']

export type Category = components['schemas']['Category']
export type CategoryKind = Category['type']

export function listAccounts(): Promise<Account[]> {
  return apiFetch<Account[]>('/api/v1/accounts')
}

export function listCategories(): Promise<Category[]> {
  return apiFetch<Category[]>('/api/v1/categories')
}

// --- issue #60: transaction recording --------------------------------

// RecordTransactionInput/RecordTransferInput carry the amount, date, and
// every other value the API normalises server-side (ADR-0005) as raw
// strings — this file never parses or validates them, it only passes them
// through (docs/architecture.md §3).
export type RecordTransactionInput = {
  account: string
  category: string
  amount: string
  date?: string
  description?: string
  notes?: string
  tags?: string[]
}

export type RecordTransferInput = {
  fromAccount: string
  toAccount: string
  amount: string
  date?: string
  description?: string
  notes?: string
  tags?: string[]
}

export function recordOutflow(
  input: RecordTransactionInput,
): Promise<Transaction> {
  return apiFetch<Transaction>('/api/v1/transactions', {
    method: 'POST',
    body: JSON.stringify({
      type: 'outflow',
      account: input.account,
      category: input.category,
      amount: input.amount,
      date: input.date,
      description: input.description ?? '',
      notes: input.notes,
      tags: input.tags,
    }),
  })
}

export function recordInflow(
  input: RecordTransactionInput,
): Promise<Transaction> {
  return apiFetch<Transaction>('/api/v1/transactions', {
    method: 'POST',
    body: JSON.stringify({
      type: 'inflow',
      account: input.account,
      category: input.category,
      amount: input.amount,
      date: input.date,
      description: input.description ?? '',
      notes: input.notes,
      tags: input.tags,
    }),
  })
}

export function recordTransfer(
  input: RecordTransferInput,
): Promise<Transaction> {
  return apiFetch<Transaction>('/api/v1/transfers', {
    method: 'POST',
    body: JSON.stringify({
      from_account: input.fromAccount,
      to_account: input.toAccount,
      amount: input.amount,
      date: input.date,
      description: input.description ?? '',
      notes: input.notes,
      tags: input.tags,
    }),
  })
}
