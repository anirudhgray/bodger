// Component tests for the settings screen (issue #62). web/src/lib/settings
// is mocked here so these exercise only the screen's own logic against a
// controlled fake API, the same pattern Login.test.tsx uses for
// web/src/lib/session.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/settings', () => ({
  changePassword: vi.fn(),
  listApiTokens: vi.fn(),
  createApiToken: vi.fn(),
  revokeApiToken: vi.fn(),
  listAccounts: vi.fn(),
  createAccount: vi.fn(),
  renameAccount: vi.fn(),
  archiveAccount: vi.fn(),
  listCategories: vi.fn(),
  createCategory: vi.fn(),
  renameCategory: vi.fn(),
  reparentCategory: vi.fn(),
  archiveCategory: vi.fn(),
}))

import { ApiError } from '@/lib/api'
import {
  archiveAccount,
  changePassword,
  createApiToken,
  listAccounts,
  listApiTokens,
  listCategories,
  revokeApiToken,
} from '@/lib/settings'
import { Settings } from './Settings'

const mockedChangePassword = vi.mocked(changePassword)
const mockedListApiTokens = vi.mocked(listApiTokens)
const mockedCreateApiToken = vi.mocked(createApiToken)
const mockedRevokeApiToken = vi.mocked(revokeApiToken)
const mockedListAccounts = vi.mocked(listAccounts)
const mockedListCategories = vi.mocked(listCategories)
const mockedArchiveAccount = vi.mocked(archiveAccount)

function renderSettings() {
  render(
    <MemoryRouter initialEntries={['/settings']}>
      <Routes>
        <Route path="/settings" element={<Settings />} />
        <Route path="/login" element={<div>Login screen</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('Settings', () => {
  beforeEach(() => {
    mockedChangePassword.mockReset()
    mockedListApiTokens.mockReset().mockResolvedValue([])
    mockedCreateApiToken.mockReset()
    mockedRevokeApiToken.mockReset()
    mockedListAccounts.mockReset().mockResolvedValue([])
    mockedListCategories.mockReset().mockResolvedValue([])
    mockedArchiveAccount.mockReset()
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
    renderSettings()

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
    renderSettings()

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

  it('changes the password and redirects to login, since the session is revoked', async () => {
    mockedChangePassword.mockResolvedValue(undefined)
    renderSettings()

    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'new-password-123' },
    })
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'new-password-123' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }))

    expect(await screen.findByText('Login screen')).toBeInTheDocument()
    expect(mockedChangePassword).toHaveBeenCalledWith('new-password-123')
  })

  it('rejects a password change when the confirmation does not match', async () => {
    renderSettings()

    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'new-password-123' },
    })
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'something-else' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('didn’t match')
    expect(mockedChangePassword).not.toHaveBeenCalled()
  })

  it('shows the server error on a failed password change', async () => {
    mockedChangePassword.mockRejectedValue(
      new ApiError(
        'invalid_input',
        'A password must be at least 8 characters.',
      ),
    )
    renderSettings()

    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'short' },
    })
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'short' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'A password must be at least 8 characters.',
    )
  })

  it('only offers same-kind categories as a Parent option', async () => {
    mockedListCategories.mockReset().mockResolvedValue([
      {
        id: 'cat_dining',
        name: 'Dining',
        type: 'expense',
        sort_order: 0,
        archived: false,
      },
      {
        id: 'cat_groceries',
        name: 'Groceries',
        type: 'expense',
        sort_order: 1,
        archived: false,
      },
      {
        id: 'cat_salary',
        name: 'Salary',
        type: 'income',
        sort_order: 0,
        archived: false,
      },
    ])
    renderSettings()

    const parentSelect = await screen.findByLabelText('Parent', {
      selector: '#parent-cat_dining',
    })
    const optionLabels = Array.from(
      parentSelect.querySelectorAll('option'),
    ).map((o) => o.textContent)
    // Dining (expense) may become top-level or sit under Groceries
    // (expense), but never under Salary (income) — one typed tree,
    // data-model.md §6.
    expect(optionLabels).toEqual(['Top level', 'Groceries'])
  })

  it('archives an account and removes it from the list', async () => {
    mockedListAccounts
      .mockResolvedValueOnce([
        {
          id: 'acc_1',
          name: 'Checking',
          type: 'bank',
          currency: 'USD',
          opening_balance: '0.00',
          sort_order: 0,
          archived: false,
        },
      ])
      .mockResolvedValueOnce([
        {
          id: 'acc_1',
          name: 'Checking',
          type: 'bank',
          currency: 'USD',
          opening_balance: '0.00',
          sort_order: 0,
          archived: true,
        },
      ])
    mockedArchiveAccount.mockResolvedValue({
      id: 'acc_1',
      name: 'Checking',
      type: 'bank',
      currency: 'USD',
      opening_balance: '0.00',
      sort_order: 0,
      archived: true,
    })
    renderSettings()

    const archiveButton = await screen.findByRole('button', {
      name: 'Archive',
    })
    fireEvent.click(archiveButton)

    await waitFor(() =>
      expect(mockedArchiveAccount).toHaveBeenCalledWith('acc_1'),
    )
    await waitFor(() =>
      expect(screen.queryByText('Checking')).not.toBeInTheDocument(),
    )
  })
})
