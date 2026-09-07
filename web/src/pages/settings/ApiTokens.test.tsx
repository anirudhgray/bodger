// Component tests for the API tokens settings subpage (issue #107, split
// out of the old Settings.test.tsx). web/src/lib/settings is mocked here
// so these exercise only the page's own logic against a controlled fake
// API.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/settings', () => ({
  listApiTokens: vi.fn(),
  createApiToken: vi.fn(),
  revokeApiToken: vi.fn(),
}))

// Create/revoke are user-triggered actions, so their failures now report
// via a toast rather than inline (docs/design-system.md's "Toasts vs.
// inline messages") — mocked here so tests can assert on what was shown
// without a real <Toaster/> mounted.
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    promise: vi.fn(),
  },
}))

import { ApiError } from '@/lib/api'
import { createApiToken, listApiTokens, revokeApiToken } from '@/lib/settings'
import { ApiTokensSettings } from './ApiTokens'
import { toast } from 'sonner'

const mockedListApiTokens = vi.mocked(listApiTokens)
const mockedCreateApiToken = vi.mocked(createApiToken)
const mockedRevokeApiToken = vi.mocked(revokeApiToken)
const mockedToastError = vi.mocked(toast.error)
const mockedToastPromise = vi.mocked(toast.promise)

describe('ApiTokensSettings', () => {
  beforeEach(() => {
    mockedListApiTokens.mockReset().mockResolvedValue([])
    mockedCreateApiToken.mockReset()
    mockedRevokeApiToken.mockReset()
    mockedToastError.mockReset()
    mockedToastPromise.mockReset()
  })

  it('shows a created token exactly once, then hides it on dismiss', async () => {
    mockedCreateApiToken.mockResolvedValue({
      id: 'tok_1',
      name: 'my-script',
      created_at: '2026-01-01T00:00:00Z',
      token: 'bdg_plaintextvalue',
    })
    mockedListApiTokens
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([
        { id: 'tok_1', name: 'my-script', created_at: '2026-01-01T00:00:00Z' },
      ])
    render(<ApiTokensSettings />)

    fireEvent.change(screen.getByLabelText('New token name'), {
      target: { value: 'my-script' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    expect(await screen.findByText('bdg_plaintextvalue')).toBeInTheDocument()
    expect(mockedCreateApiToken).toHaveBeenCalledWith('my-script')

    fireEvent.click(
      screen.getByRole('button', { name: 'I’ve copied or stored it' }),
    )
    expect(screen.queryByText('bdg_plaintextvalue')).not.toBeInTheDocument()

    // The list itself never carries the plaintext value — only the
    // one-time creation response does.
    expect(
      await screen.findByRole('button', { name: 'Revoke' }),
    ).toBeInTheDocument()
    expect(screen.queryByText('bdg_plaintextvalue')).not.toBeInTheDocument()
  })

  it('shows a toast (not inline) when creating a token fails', async () => {
    mockedCreateApiToken.mockRejectedValue(
      new ApiError('invalid_input', 'A token named "my-script" already exists.'),
    )
    render(<ApiTokensSettings />)

    fireEvent.change(screen.getByLabelText('New token name'), {
      target: { value: 'my-script' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    // Creating an API token is a user-triggered action
    // (docs/design-system.md's "Toasts vs. inline messages"), so its
    // failure is reported via a toast, not inline.
    await waitFor(() =>
      expect(mockedToastError).toHaveBeenCalledWith(
        'A token named "my-script" already exists.',
      ),
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('revokes a token and removes its revoke button', async () => {
    mockedListApiTokens
      .mockResolvedValueOnce([
        { id: 'tok_1', name: 'my-script', created_at: '2026-01-01T00:00:00Z' },
      ])
      .mockResolvedValueOnce([
        {
          id: 'tok_1',
          name: 'my-script',
          created_at: '2026-01-01T00:00:00Z',
          revoked_at: '2026-01-02T00:00:00Z',
        },
      ])
    mockedRevokeApiToken.mockResolvedValue(undefined)
    render(<ApiTokensSettings />)

    const revokeButton = await screen.findByRole('button', { name: 'Revoke' })
    fireEvent.click(revokeButton)

    await waitFor(() =>
      expect(mockedRevokeApiToken).toHaveBeenCalledWith('tok_1'),
    )
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: 'Revoke' }),
      ).not.toBeInTheDocument(),
    )
  })

  it('leaves the revoke button in place and reports a toast (not inline) when revoke fails', async () => {
    mockedListApiTokens.mockResolvedValue([
      { id: 'tok_1', name: 'my-script', created_at: '2026-01-01T00:00:00Z' },
    ])
    mockedRevokeApiToken.mockRejectedValue(
      new ApiError('internal', 'Couldn’t reach the server. Try again.'),
    )
    render(<ApiTokensSettings />)

    const revokeButton = await screen.findByRole('button', { name: 'Revoke' })
    fireEvent.click(revokeButton)

    await waitFor(() => expect(mockedToastPromise).toHaveBeenCalled())
    // Revoking a token is a user-triggered action (docs/design-
    // system.md's "Toasts vs. inline messages"), so its failure is
    // reported via toast.promise's `error` option, not inline.
    const [, options] = mockedToastPromise.mock.calls[0]
    expect(
      (options?.error as (err: unknown) => string)(
        new ApiError('internal', 'Couldn’t reach the server. Try again.'),
      ),
    ).toBe('Couldn’t reach the server. Try again.')

    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Revoke' }),
      ).toBeInTheDocument(),
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
