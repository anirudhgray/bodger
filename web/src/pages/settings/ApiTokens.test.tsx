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

import { createApiToken, listApiTokens, revokeApiToken } from '@/lib/settings'
import { ApiTokensSettings } from './ApiTokens'

const mockedListApiTokens = vi.mocked(listApiTokens)
const mockedCreateApiToken = vi.mocked(createApiToken)
const mockedRevokeApiToken = vi.mocked(revokeApiToken)

describe('ApiTokensSettings', () => {
  beforeEach(() => {
    mockedListApiTokens.mockReset().mockResolvedValue([])
    mockedCreateApiToken.mockReset()
    mockedRevokeApiToken.mockReset()
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
})
