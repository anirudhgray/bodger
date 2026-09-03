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
// client-side financial logic, docs/architecture.md §3).
export type BalanceEntry = {
  account_id: string
  account: string
  amount: string
  currency: string
}

export type Balances = {
  as_of: string
  balances: BalanceEntry[]
}

// getBalances fetches every account's balance as of today (the API
// resolves "today" server-side when as_of is omitted — normalise-once,
// ADR-0005 — this call never passes one).
export function getBalances(): Promise<Balances> {
  return apiFetch<Balances>('/api/v1/balances')
}

// --- issue #60: accounts/categories lookups and transaction recording ---
// Added as new, self-contained functions rather than touching apiFetch or
// anything above — several other issues (#61-#63) are adding their own
// functions to this file at the same time, on sibling branches.

export type Account = {
  id: string
  name: string
  type: string
  currency: string
  archived: boolean
}

export type Category = {
  id: string
  name: string
  type: 'expense' | 'income'
  archived: boolean
}

export function listAccounts(): Promise<Account[]> {
  return apiFetch<Account[]>('/api/v1/accounts')
}

export function listCategories(): Promise<Category[]> {
  return apiFetch<Category[]>('/api/v1/categories')
}

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

export function recordOutflow(input: RecordTransactionInput): Promise<unknown> {
  return apiFetch('/api/v1/transactions', {
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

export function recordInflow(input: RecordTransactionInput): Promise<unknown> {
  return apiFetch('/api/v1/transactions', {
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

export function recordTransfer(input: RecordTransferInput): Promise<unknown> {
  return apiFetch('/api/v1/transfers', {
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
