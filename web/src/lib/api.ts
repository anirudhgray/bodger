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

// BalancesQuery mirrors GET /api/v1/balances' optional query params
// (internal/surface/http/router.go): setting currency converts every
// account's balance into it under policy (ADR-0004) — issue #139's
// Balances screen always passes policy: 'current' (a point-in-time
// snapshot has no other sensible date), so pinnedDate is only here for
// completeness with the wire contract, not because this screen uses it.
export type BalancesQuery = {
  currency?: string
  policy?: components['schemas']['ConvertedBalance']['policy']
  pinnedDate?: string
}

// getBalances fetches every account's balance as of today (the API
// resolves "today" server-side when as_of is omitted — normalise-once,
// ADR-0005 — this call never passes one).
export function getBalances(query: BalancesQuery = {}): Promise<Balances> {
  return apiFetch<Balances>(
    `/api/v1/balances${buildQuery({
      currency: query.currency,
      policy: query.policy,
      pinned_date: query.pinnedDate,
    })}`,
  )
}

// BalanceTotals is issue #195's totals-overview shape: the overall net
// balance and per-category breakdown, both converted into "currency"
// (server-resolved to the actor's reporting currency when this call
// leaves it unset), plus the per-currency breakdown, which is always raw
// and unconverted — and, when the requested conversion couldn't cover
// every account, the same "unconverted" shortfall list GET
// /api/v1/balances already reports (never dropped silently).
export type BalanceTotals = components['schemas']['BalanceTotals']

// getBalanceTotals fetches issue #195's totals overview as of today.
// Unlike getBalances, "policy" is required here (mirroring the M5
// analytics endpoints' own "always a converted aggregate" shape) — this
// screen always passes 'current', the only sensible policy for a
// present-tense "what do I have" question.
export function getBalanceTotals(
  policy: BalancesQuery['policy'],
  currency?: string,
): Promise<BalanceTotals> {
  return apiFetch<BalanceTotals>(
    `/api/v1/balances/totals${buildQuery({ currency, policy })}`,
  )
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
  // The to-leg's own amount on a transfer, independent of `amount`
  // (issue #159) — see RecordTransferInput's toAmount for what omitting
  // it means.
  to_amount?: string
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
  // The to-leg's own amount, independent of `amount` (issue #159) — when
  // given, the implied rate is derived from these two real amounts
  // instead of `amount`'s raw digits reused in the to-currency. Omit for
  // same-currency transfers, where there is no separate to-leg amount to
  // state.
  toAmount?: string
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
      to_amount: input.toAmount,
      date: input.date,
      description: input.description ?? '',
      notes: input.notes,
      tags: input.tags,
    }),
  })
}

// --- FX rates and reporting currency (issue #138) --------------------------
//
// Every lookup here only ever resolves a rate FROM a foreign currency TO
// the reporting currency: internal/adapters/sqlite/fx_rate_repo.go's
// Lookup does an exact base/quote match against stored rows, and POST
// /api/v1/fx/rates/fetch only ever stores <foreign>/<reporting-currency>
// rows (internal/app/fetch_fx_rates.go's resolveFetchPairs always quotes
// against the resolved reporting currency) — there is no reverse lookup
// and no support for a pair between two non-reporting currencies yet.
// Callers pass whatever "to" they actually need and handle a 404
// (ApiError with code "not_found") as "no rate available", rather than
// this file pre-guessing which directions will resolve.

export type FxRate = components['schemas']['FxRate']

export type FxRateQuery = {
  from: string
  to: string
  amount?: string
} & (
  | { policy: 'current' }
  | { policy: 'transaction_date'; transactionDate: string }
)

// getFxRate is GET /api/v1/fx/rates: a pure, stored-data-only read (issue
// #135) — it never triggers a network fetch of its own, unlike
// fetchFxRates below.
export function getFxRate(query: FxRateQuery): Promise<FxRate> {
  return apiFetch<FxRate>(
    `/api/v1/fx/rates${buildQuery({
      from: query.from,
      to: query.to,
      amount: query.amount,
      policy: query.policy,
      transaction_date:
        query.policy === 'transaction_date' ? query.transactionDate : undefined,
    })}`,
  )
}

export type FxFetchResult = components['schemas']['FxFetch']

// fetchFxRates is POST /api/v1/fx/rates/fetch, scoped to exactly the
// given base currencies — never the broad "every in-use pair, latest
// date" default, matching issue #138's narrowly-scoped refresh action.
// `date` is either a single day (a string, backfilling just that one
// date — issue #138/#139's own refresh actions) or a `{ from, to }`
// range (issue #145's TransactionsList backfill popover, over the
// date-range mode #135/#137 already implemented server-side); omit it
// entirely to keep today's default. quote (issue #165) fetches pairs
// against that currency instead of the reporting currency — omit it to
// keep the reporting-currency default.
export function fetchFxRates(
  pairs: string[],
  date?: string | { from: string; to: string },
  quote?: string,
): Promise<FxFetchResult> {
  const range = typeof date === 'string' ? { from: date, to: date } : date
  return apiFetch<FxFetchResult>('/api/v1/fx/rates/fetch', {
    method: 'POST',
    body: JSON.stringify({
      pairs,
      quote,
      from: range?.from,
      to: range?.to,
    }),
  })
}

// ReportingCurrency mirrors GET /api/v1/reporting-currency's response
// (internal/surface/http/config.go's reportingCurrencyView): is_set
// distinguishes "never configured" (currency is "") from an actual
// choice, since the instance default currency isn't itself exposed over
// this API (only the CLI, which runs server-side, can name it — see
// internal/surface/cli/config.go's printReportingCurrency).
// effectiveCurrency (issue #170) is always populated — the actor-set
// value when is_set, otherwise the resolved instance default — so a UI
// can always show something rather than gating on is_set itself.
export type ReportingCurrency = components['schemas']['ReportingCurrency']

export function getReportingCurrency(): Promise<ReportingCurrency> {
  return apiFetch<ReportingCurrency>('/api/v1/reporting-currency')
}

// setReportingCurrency lives in lib/settings.ts, alongside the rest of
// the settings screen's write calls — this file's get/set split for
// reporting currency mirrors listAccounts/listCategories (read, used by
// several screens) living here while their own create/archive calls stay
// in lib/settings.ts.

// --- Analytics (issue #189) ------------------------------------------
//
// Typed helpers over the four GET /api/v1/analytics/* endpoints
// (internal/surface/http/analytics.go). The web screen only ever exposes
// a date range and a reporting currency as filter chrome (issue #189's
// own scope) — the full ADR-0009 filter dimensions these endpoints
// accept (account/category/type/amount/description/tags) stay CLI/API-
// only until a screen actually needs them.

export type AnalyticsFilter = {
  from?: string
  to?: string
}

export type ConversionPolicy = 'transaction_date' | 'current' | 'pinned'

export type AnalyticsOptions = {
  currency: string
  policy: ConversionPolicy
  pinnedDate?: string
}

// Granularity (issue #194) selects how CashFlow/Trends bucket/compare
// periods server-side — see internal/app/analytics.go's Granularity.
// CategoryBreakdown/SavingsRate take no granularity (out of scope).
export type Granularity = 'week' | 'month' | 'year' | 'custom'

function analyticsQuery(
  filter: AnalyticsFilter,
  options: AnalyticsOptions,
  granularity?: Granularity,
) {
  return buildQuery({
    from: filter.from,
    to: filter.to,
    currency: options.currency,
    policy: options.policy,
    pinned_date: options.pinnedDate,
    granularity,
  })
}

export type CategoryBreakdown = components['schemas']['CategoryBreakdown']
export type CashFlow = components['schemas']['CashFlow']
export type Trends = components['schemas']['Trends']
export type SavingsRate = components['schemas']['SavingsRate']

export function getCategoryBreakdown(
  filter: AnalyticsFilter,
  options: AnalyticsOptions,
): Promise<CategoryBreakdown> {
  return apiFetch<CategoryBreakdown>(
    `/api/v1/analytics/category-breakdown${analyticsQuery(filter, options)}`,
  )
}

export function getCashFlow(
  filter: AnalyticsFilter,
  options: AnalyticsOptions,
  granularity?: Granularity,
): Promise<CashFlow> {
  return apiFetch<CashFlow>(
    `/api/v1/analytics/cash-flow${analyticsQuery(filter, options, granularity)}`,
  )
}

// getTrends ignores any date range except under granularity "custom" —
// week/month/year define their own current-vs-previous comparison
// window server-side (issue #187; #194's granularity selector).
export function getTrends(
  options: AnalyticsOptions,
  granularity?: Granularity,
  filter: AnalyticsFilter = {},
): Promise<Trends> {
  return apiFetch<Trends>(
    `/api/v1/analytics/trends${analyticsQuery(filter, options, granularity)}`,
  )
}

export function getSavingsRate(
  filter: AnalyticsFilter,
  options: AnalyticsOptions,
): Promise<SavingsRate> {
  return apiFetch<SavingsRate>(
    `/api/v1/analytics/savings-rate${analyticsQuery(filter, options)}`,
  )
}

// --- Additional analytics (issue #196) -------------------------------------
//
// Net worth over time, top transactions, average transaction size, and
// per-category trend deltas — extending M5's baseline four (above) and
// #195's point-in-time BalanceTotals. Same "server computes, screen only
// renders" contract: every figure below is a decimal string or a
// server-computed percentage, never derived here.

export type TopTransactions = components['schemas']['TopTransactions']

export function getTopTransactions(
  filter: AnalyticsFilter,
  options: AnalyticsOptions,
  limit?: number,
): Promise<TopTransactions> {
  return apiFetch<TopTransactions>(
    `/api/v1/analytics/top-transactions${buildQuery({
      from: filter.from,
      to: filter.to,
      currency: options.currency,
      policy: options.policy,
      pinned_date: options.pinnedDate,
      limit: limit ? String(limit) : undefined,
    })}`,
  )
}

export type AverageTransactionSize =
  components['schemas']['AverageTransactionSize']

export function getAverageTransactionSize(
  filter: AnalyticsFilter,
  options: AnalyticsOptions,
): Promise<AverageTransactionSize> {
  return apiFetch<AverageTransactionSize>(
    `/api/v1/analytics/average-transaction-size${analyticsQuery(filter, options)}`,
  )
}

export type CategoryTrends = components['schemas']['CategoryTrends']

export function getCategoryTrends(
  filter: AnalyticsFilter,
  options: AnalyticsOptions,
  granularity?: Granularity,
): Promise<CategoryTrends> {
  return apiFetch<CategoryTrends>(
    `/api/v1/analytics/category-trends${analyticsQuery(filter, options, granularity)}`,
  )
}

// NetWorthOverTime (issue #196) lives on the Balances screen, not
// Analytics — it's a time series over the whole ledger, distinct from
// BalanceTotals' own point-in-time snapshot (getBalanceTotals above).
export type NetWorthOverTime = components['schemas']['NetWorthOverTime']

export function getNetWorthOverTime(
  filter: { from: string; to: string },
  policy: BalancesQuery['policy'],
  currency?: string,
  granularity?: Granularity,
): Promise<NetWorthOverTime> {
  return apiFetch<NetWorthOverTime>(
    `/api/v1/balances/net-worth-over-time${buildQuery({
      from: filter.from,
      to: filter.to,
      currency,
      policy,
      granularity,
    })}`,
  )
}

// --- Import (issue #214, wrapping #212's REST surface) ---------------------
//
// ADR-0008's staged pipeline: upload stages a file's rows for review
// without touching the ledger, resolving a suspected duplicate clears (or
// excludes) a row, and only commit ever writes real transactions. This
// file's functions are thin wrappers over #212's routes — no parsing,
// mapping, or duplicate-detection logic lives here (docs/architecture.md
// §3's "no financial logic in the web UI").

export type ImportBatch = components['schemas']['ImportBatch']
export type ImportBatchStatus = ImportBatch['status']
export type ImportRecord = components['schemas']['ImportRecord']
export type ImportRecordStatus = ImportRecord['status']
export type DuplicateMatch = components['schemas']['DuplicateMatch']
export type ImportDuplicateResolution = DuplicateMatch['resolution']
export type ImportBatchWithRecords =
  components['schemas']['ImportBatchWithRecords']
export type ImportCommit = components['schemas']['ImportCommit']
export type ImportRollback = components['schemas']['ImportRollback']

// ImportColumnMapping mirrors POST /api/v1/imports' own query parameters
// (internal/surface/http/imports.go's columnMappingFromQuery) field for
// field: raw column names read straight off the uploaded file's own
// header row. Nothing here is validated or resolved client-side — the
// app layer does that once the upload actually lands (ADR-0005's
// normalise-once rule applies to a wizard step the same as any other
// surface).
export type ImportColumnMapping = {
  dateColumn: string
  descriptionColumn: string
  amountColumn: string
  postedDateColumn?: string
  currencyColumn?: string
  externalIDColumn?: string
  categoryColumn?: string
}

export type UploadImportInput = {
  account: string
  filename: string
  fileContent: string
  mapping: ImportColumnMapping
}

// uploadImport is POST /api/v1/imports: the request body is the uploaded
// file's own raw bytes, not JSON (mirroring the server's own raw-request
// convention for this one route) — account, filename, and the column
// mapping travel as query parameters instead. apiFetch's default
// Content-Type would be "application/json"; this call overrides it since
// the body is CSV text, not a JSON string.
export function uploadImport(
  input: UploadImportInput,
): Promise<ImportBatchWithRecords> {
  const query = buildQuery({
    account: input.account,
    filename: input.filename,
    format: 'csv',
    date_column: input.mapping.dateColumn,
    description_column: input.mapping.descriptionColumn,
    amount_column: input.mapping.amountColumn,
    posted_date_column: input.mapping.postedDateColumn,
    currency_column: input.mapping.currencyColumn,
    external_id_column: input.mapping.externalIDColumn,
    category_column: input.mapping.categoryColumn,
  })
  return apiFetch<ImportBatchWithRecords>(`/api/v1/imports${query}`, {
    method: 'POST',
    headers: { 'Content-Type': 'text/csv' },
    body: input.fileContent,
  })
}

// listImportBatches lists every import, most recently created first (the
// order the API itself already guarantees — nothing is re-sorted here).
export function listImportBatches(): Promise<ImportBatch[]> {
  return apiFetch<ImportBatch[]>('/api/v1/imports')
}

export function getImportBatch(id: string): Promise<ImportBatch> {
  return apiFetch<ImportBatch>(`/api/v1/imports/${id}`)
}

export function listImportRecords(batchId: string): Promise<ImportRecord[]> {
  return apiFetch<ImportRecord[]>(`/api/v1/imports/${batchId}/records`)
}

export function commitImportBatch(id: string): Promise<ImportCommit> {
  return apiFetch<ImportCommit>(`/api/v1/imports/${id}/commit`, {
    method: 'POST',
  })
}

export function rollbackImportBatch(id: string): Promise<ImportRollback> {
  return apiFetch<ImportRollback>(`/api/v1/imports/${id}/rollback`, {
    method: 'POST',
  })
}

// ResolvableImportResolution excludes "pending" — that's a staged
// record's own starting state, never something a caller resolves it back
// to (internal/surface/http/imports.go's resolveImportRecordRequest doc
// comment: only "confirmed_duplicate" or "not_duplicate" are valid
// decisions).
export type ResolvableImportResolution = Exclude<
  ImportDuplicateResolution,
  'pending'
>

export function resolveImportRecord(
  id: string,
  resolution: ResolvableImportResolution,
): Promise<ImportRecord> {
  return apiFetch<ImportRecord>(`/api/v1/import-records/${id}/resolve`, {
    method: 'POST',
    body: JSON.stringify({ resolution }),
  })
}

// --- Export and restore (issues #213, #227) ---------------------------
//
// Both /api/v1/export/* routes and /api/v1/restore move whole documents
// rather than the usual small JSON objects: export's response body and
// restore's own upload are the canonical bodger.export/v1 document itself
// (ADR-0008), not this API's usual {"data": ...} envelope. downloadFile
// below is this file's equivalent of apiFetch for that shape — same
// ApiError on failure, but the success body is a Blob, not parsed JSON.

export type ExportCSVFilter = {
  account?: string
  category?: string
  type?: TransactionKind
  from?: string
  to?: string
}

// downloadFile fetches path and returns its raw response body as a Blob,
// throwing the same ApiError apiFetch does on a non-2xx response (the
// error envelope is identical; only the success body's shape differs
// here, per this section's own doc comment above).
async function downloadFile(path: string): Promise<Blob> {
  const res = await fetch(path, { credentials: 'include' })
  if (!res.ok) {
    let errBody: ErrorBody | undefined
    try {
      errBody = (await res.json()) as ErrorBody
    } catch {
      errBody = undefined
    }
    throw new ApiError(
      errBody?.error?.code ?? 'internal',
      errBody?.error?.message ?? 'Something went wrong. Try again in a moment.',
      errBody?.error?.field,
    )
  }
  return res.blob()
}

// saveBlob triggers a browser "Save As" for blob without navigating the
// SPA away from the current page — there's no server-rendered <a> to
// just click here, so this is the client-side equivalent.
function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

// downloadExportJSON downloads the complete canonical JSON backup
// (ADR-0008) — every account, category, and transaction the actor owns,
// unfiltered, deterministic across repeated calls against the same data.
export async function downloadExportJSON(): Promise<void> {
  const blob = await downloadFile('/api/v1/export/json')
  saveBlob(blob, 'bodger-export.json')
}

// downloadExportCSV downloads transactions as CSV, one row per posting,
// optionally narrowed by the same filter dimensions the transaction list
// itself already filters by. Flat and lossy by design (ADR-0008): not an
// interchange format, for spreadsheets only — a split transaction can't
// round-trip through it.
export async function downloadExportCSV(
  filter: ExportCSVFilter = {},
): Promise<void> {
  const blob = await downloadFile(`/api/v1/export/csv${buildQuery(filter)}`)
  saveBlob(blob, 'bodger-export.csv')
}

export type RestoreSnapshotResult = components['schemas']['RestoreSnapshot']

// restoreSnapshot is POST /api/v1/restore: wipes and reloads the actor's
// entire ledger — every account, category, and transaction — from
// document, a canonical JSON backup exactly as GET /api/v1/export/json
// produces. confirm has no default and must be passed as literal `true`:
// the server itself refuses the request without it (restore.go), and
// requiring it here too means a caller can't accidentally wire this up
// to fire without its own explicit confirmation step.
export function restoreSnapshot(
  document: unknown,
  confirm: true,
): Promise<RestoreSnapshotResult> {
  return apiFetch<RestoreSnapshotResult>('/api/v1/restore', {
    method: 'POST',
    body: JSON.stringify({ document, confirm }),
  })
}
