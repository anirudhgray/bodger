// Session handling (issue #59) on top of api.ts's transport: login,
// logout, and a way for a route loader to ask "is the cookie in hand
// still good" before rendering anything behind it. None of this stores a
// token anywhere the web UI can read — the session credential is an
// HttpOnly cookie the browser manages entirely on its own
// (internal/surface/http/auth_middleware.go, ADR-0006).
import { apiFetch } from '@/lib/api'

// login exchanges a password for a session: on success the API has
// already set the session cookie via Set-Cookie, so there's nothing else
// for the caller to store.
export async function login(password: string): Promise<void> {
  await apiFetch<{ actor_id: string }>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}

// logout revokes the session that's currently authenticating this
// browser and clears its cookie server-side.
export async function logout(): Promise<void> {
  await apiFetch('/api/v1/auth/logout', { method: 'POST' })
}

// checkSession reports whether the browser currently holds a valid
// session cookie. There's no dedicated "who am I" endpoint — out of scope
// for this web-only issue, since adding one is a REST API change (#56's
// territory, already shipped without one) — so this reuses a real
// protected route we already need to authenticate against:
// GET /api/v1/auth/tokens. It's a safe (GET) request, needs no CSRF
// header, and its response is discarded here; a 2xx means the cookie
// authenticates, anything else (in practice, a 401) means it doesn't.
export async function checkSession(): Promise<boolean> {
  try {
    await apiFetch('/api/v1/auth/tokens')
    return true
  } catch {
    return false
  }
}
