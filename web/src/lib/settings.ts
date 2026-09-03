// Typed REST calls for the settings screen (issue #62): password change,
// API tokens, and accounts/categories CRUD. Each function wraps exactly
// one existing (or #62-added) application-layer call, matching this
// project's one-call-per-action discipline — no business logic lives
// here, only the shape of the request/response bodies
// internal/surface/http already defines (see dto.go, auth.go).
import { apiFetch } from '@/lib/api'

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

export type ApiToken = {
  id: string
  name: string
  created_at: string
  last_used_at?: string
  expires_at?: string
  revoked_at?: string
}

export type CreatedApiToken = ApiToken & {
  token: string
}

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

// AccountKind/CategoryKind mirror internal/surface/http/openapi_gen.go's
// enum table (the real wire values ledger.AccountKind/CategoryKind
// produce) — kept here rather than imported from '@/lib/api' because this
// branch predates #60/#61 (issues #60, #61, #62 each independently needed
// this same reference data on separate branches); once this stack merges,
// this and api.ts's identical definitions should collapse into one.
export type AccountKind =
  'bank' | 'cash' | 'credit_card' | 'wallet' | 'investment' | 'loan' | 'other'

export type Account = {
  id: string
  name: string
  type: AccountKind
  currency: string
  opening_balance: string
  opening_balance_date?: string
  institution?: string
  sort_order: number
  archived: boolean
  archived_at?: string
}

export async function listAccounts(): Promise<Account[]> {
  return apiFetch<Account[]>('/api/v1/accounts')
}

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

export type CategoryKind = 'expense' | 'income'

export type Category = {
  id: string
  name: string
  type: CategoryKind
  parent_id?: string
  sort_order: number
  archived: boolean
  archived_at?: string
}

export async function listCategories(): Promise<Category[]> {
  return apiFetch<Category[]>('/api/v1/categories')
}

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
