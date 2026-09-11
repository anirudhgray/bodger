// Typed REST calls for the settings screen (issue #62): password change,
// API tokens, and accounts/categories CRUD. Each function wraps exactly
// one existing (or #62-added) application-layer call, matching this
// project's one-call-per-action discipline — no business logic lives
// here, only the shape of the request/response bodies
// internal/surface/http already defines (see dto.go, auth.go).
import { apiFetch, type Account, type Category } from '@/lib/api'
import type { components } from '@/lib/api-types'

// changePassword calls the in-app password-change route (#62). The API
// revokes every session on success, including the one that made this
// request (internal/surface/http/auth.go's changePassword doc comment) —
// the caller is responsible for treating the browser as logged out
// afterward.
export async function changePassword(newPassword: string): Promise<void> {
  await apiFetch('/api/v1/auth/password', {
    method: 'POST',
    body: JSON.stringify({ new_password: newPassword }),
  })
}

// getReportingCurrency/ReportingCurrency live in lib/api.ts (issue
// #183) — this file only adds the write side.
export async function setReportingCurrency(currency: string): Promise<void> {
  await apiFetch('/api/v1/reporting-currency', {
    method: 'POST',
    body: JSON.stringify({ currency }),
  })
}

// ApiToken/CreatedApiToken are generated from openapi.json (issue #83),
// not hand-transcribed — see the Account/Category comment further down
// for why that matters. CreatedApiToken (POST's response) genuinely
// isn't just ApiToken plus a token field: it lacks last_used_at/revoked_at,
// since a token this response describes was just created.
export type ApiToken = components['schemas']['ApiToken']

export type CreatedApiToken = components['schemas']['CreateAPITokenResponse']

export async function listApiTokens(): Promise<ApiToken[]> {
  return apiFetch<ApiToken[]>('/api/v1/auth/tokens')
}

export async function createApiToken(
  name: string,
  expiresAt?: string,
): Promise<CreatedApiToken> {
  return apiFetch<CreatedApiToken>('/api/v1/auth/tokens', {
    method: 'POST',
    body: JSON.stringify({ name, expires_at: expiresAt || undefined }),
  })
}

export async function revokeApiToken(id: string): Promise<void> {
  await apiFetch(`/api/v1/auth/tokens/${id}`, { method: 'DELETE' })
}

// Account is imported from '@/lib/api' (see the top of this file) rather
// than declared here — issues #60/#61/#62 each independently needed this
// same reference data on separate branches and each guessed its own
// shape; issue #83 collapsed every copy onto the one generated from
// internal/surface/http/openapi.json. AccountKind isn't used by anything
// in this file, so callers that need it (e.g.
// pages/settings/Accounts.tsx) import it straight from '@/lib/api'
// rather than through a re-export here.

// listAccounts/listCategories (issue #183) live in lib/api.ts — several
// non-settings screens (TransactionsList, TransactionDialog, Balances)
// need the same read, so this file only adds the create/rename/archive
// calls below. listApiTokens has no such reader outside Settings, so it
// stays here in full.
//
// Neither listAccounts/listCategories nor listApiTokens paginate — no
// `cursor`/`limit`, unlike listTransactions's real next_cursor-based
// paging (web/src/lib/api.ts, internal/surface/http/transactions.go).
// issue #107 considered adding that here too, alongside splitting
// Settings.tsx into its own subpages per section, but decided against it
// for now: a household's account and category lists are bounded by how
// many distinct accounts/categories they bother to create by hand
// (nothing generates these in bulk, unlike transactions), and each now
// has a full page to itself rather than sharing scroll space with three
// other sections. Real pagination is still the right fix if that stops
// being true — a category tree (#106) or a household with dozens of
// accounts could get there — but adding it now, before any section
// actually needs it, would be speculative.
export async function createAccount(input: {
  name: string
  type: string
  currency?: string
}): Promise<Account> {
  return apiFetch<Account>('/api/v1/accounts', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function renameAccount(
  id: string,
  name: string,
): Promise<Account> {
  return apiFetch<Account>(`/api/v1/accounts/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ name }),
  })
}

export async function archiveAccount(id: string): Promise<Account> {
  return apiFetch<Account>(`/api/v1/accounts/${id}`, { method: 'DELETE' })
}

// Category: see the Account comment above — same reasoning, and
// CategoryKind is likewise imported straight from '@/lib/api' by callers
// that need it.

export async function createCategory(input: {
  name: string
  type: string
  parent?: string
}): Promise<Category> {
  return apiFetch<Category>('/api/v1/categories', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function renameCategory(
  id: string,
  name: string,
): Promise<Category> {
  return apiFetch<Category>(`/api/v1/categories/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ name }),
  })
}

export async function archiveCategory(id: string): Promise<Category> {
  return apiFetch<Category>(`/api/v1/categories/${id}`, { method: 'DELETE' })
}

// reparentCategory moves category id under parent, or to the top level
// when parent is "" (internal/surface/http/categories.go's
// patchCategoryRequest doc comment: an empty "parent" is itself a
// meaningful request, not "no change").
export async function reparentCategory(
  id: string,
  parent: string,
): Promise<Category> {
  return apiFetch<Category>(`/api/v1/categories/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ parent }),
  })
}
