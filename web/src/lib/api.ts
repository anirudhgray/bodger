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
