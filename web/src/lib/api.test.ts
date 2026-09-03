// Unit tests for apiFetch (issue #59): the CSRF header attachment rule
// and error-envelope unwrapping are the two pieces of real logic here —
// everything else is a thin pass-through to fetch.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { apiFetch, ApiError } from '@/lib/api'

describe('apiFetch', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    fetchMock.mockReset()
    vi.unstubAllGlobals()
  })

  function jsonResponse(status: number, body: unknown) {
    return {
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
    } as Response
  }

  it('sends credentials and no CSRF header on a GET', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { data: { ok: true } }))

    await apiFetch('/api/v1/auth/tokens')

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/v1/auth/tokens')
    expect(init.credentials).toBe('include')
    expect(new Headers(init.headers).has('X-Bodger-CSRF')).toBe(false)
  })

  it('attaches the CSRF header on a state-changing request', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { data: { ok: true } }))

    await apiFetch('/api/v1/auth/logout', { method: 'POST' })

    const [, init] = fetchMock.mock.calls[0]
    expect(new Headers(init.headers).get('X-Bodger-CSRF')).toBe('1')
  })

  it('unwraps the data envelope on success', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { data: { actor_id: 'u1' } }))

    const result = await apiFetch<{ actor_id: string }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ password: 'x' }),
    })

    expect(result).toEqual({ actor_id: 'u1' })
  })

  it('throws an ApiError built from the error envelope on failure', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(401, {
        error: {
          code: 'unauthenticated',
          message: 'Authentication is required.',
        },
      }),
    )

    await expect(apiFetch('/api/v1/auth/tokens')).rejects.toMatchObject({
      code: 'unauthenticated',
      message: 'Authentication is required.',
    })
    await expect(apiFetch('/api/v1/auth/tokens')).rejects.toBeInstanceOf(
      ApiError,
    )
  })

  it('falls back to a generic ApiError when the body has no error envelope', async () => {
    fetchMock.mockResolvedValue(jsonResponse(500, {}))

    await expect(apiFetch('/api/v1/auth/tokens')).rejects.toMatchObject({
      code: 'internal',
    })
  })
})
