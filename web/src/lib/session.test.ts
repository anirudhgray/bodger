// checkSession's own logic (issue #59) is entirely "turn any failure into
// false" — api.test.ts already covers apiFetch's request/response
// handling, so this only needs to prove that wrapping.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { checkSession } from '@/lib/session'

describe('checkSession', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
  })

  afterEach(() => {
    fetchMock.mockReset()
    vi.unstubAllGlobals()
  })

  it('reports true when the probe request succeeds', async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: [] }),
    })

    await expect(checkSession()).resolves.toBe(true)
  })

  it('reports false when the probe request is unauthenticated', async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 401,
      json: async () => ({
        error: {
          code: 'unauthenticated',
          message: 'Authentication is required.',
        },
      }),
    })

    await expect(checkSession()).resolves.toBe(false)
  })

  it('reports false when the request itself fails', async () => {
    fetchMock.mockRejectedValue(new TypeError('network error'))

    await expect(checkSession()).resolves.toBe(false)
  })
})
