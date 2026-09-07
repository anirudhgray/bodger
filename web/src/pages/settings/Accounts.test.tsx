// Component tests for the Accounts settings subpage (issue #107, split
// out of the old Settings.test.tsx). web/src/lib/settings is mocked here
// so these exercise only the page's own logic against a controlled fake
// API.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/settings', () => ({
  listAccounts: vi.fn(),
  createAccount: vi.fn(),
  renameAccount: vi.fn(),
  archiveAccount: vi.fn(),
}))

import { archiveAccount, createAccount, listAccounts } from '@/lib/settings'
import { AccountsSettings } from './Accounts'

const mockedListAccounts = vi.mocked(listAccounts)
const mockedArchiveAccount = vi.mocked(archiveAccount)
const mockedCreateAccount = vi.mocked(createAccount)

describe('AccountsSettings', () => {
  beforeEach(() => {
    mockedListAccounts.mockReset().mockResolvedValue([])
    mockedArchiveAccount.mockReset()
    mockedCreateAccount.mockReset().mockResolvedValue({
      id: 'acc_new',
      name: 'Vacation fund',
      type: 'bank',
      currency: 'EUR',
      opening_balance: '0.00',
      sort_order: 0,
      archived: false,
    })
  })

  it('creates an account with an explicit currency (issue #171)', async () => {
    const user = userEvent.setup()
    render(<AccountsSettings />)
    await screen.findByRole('button', { name: 'Add' })

    await user.type(screen.getByLabelText('New account name'), 'Vacation fund')
    await user.type(screen.getByLabelText('Currency'), 'eur')
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    await waitFor(() =>
      expect(mockedCreateAccount).toHaveBeenCalledWith({
        name: 'Vacation fund',
        type: 'bank',
        currency: 'EUR',
      }),
    )
  })

  it('omits currency entirely when left blank, falling through the ladder', async () => {
    const user = userEvent.setup()
    render(<AccountsSettings />)
    await screen.findByRole('button', { name: 'Add' })

    await user.type(screen.getByLabelText('New account name'), 'Checking')
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    await waitFor(() =>
      expect(mockedCreateAccount).toHaveBeenCalledWith({
        name: 'Checking',
        type: 'bank',
        currency: undefined,
      }),
    )
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
    render(<AccountsSettings />)

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
