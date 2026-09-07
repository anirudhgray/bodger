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

// Create/rename/archive are user-triggered actions, so their failures
// now report via a toast rather than inline (docs/design-system.md's
// "Toasts vs. inline messages") — mocked here so tests can assert on
// what was shown without a real <Toaster/> mounted.
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    promise: vi.fn(),
  },
}))

import { ApiError } from '@/lib/api'
import { archiveAccount, createAccount, listAccounts } from '@/lib/settings'
import { AccountsSettings } from './Accounts'
import { toast } from 'sonner'

const mockedListAccounts = vi.mocked(listAccounts)
const mockedArchiveAccount = vi.mocked(archiveAccount)
const mockedCreateAccount = vi.mocked(createAccount)
const mockedToastError = vi.mocked(toast.error)
const mockedToastPromise = vi.mocked(toast.promise)

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
    mockedToastError.mockReset()
    mockedToastPromise.mockReset()
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

  it('shows a toast (not inline) when creating an account fails', async () => {
    const user = userEvent.setup()
    mockedCreateAccount.mockRejectedValue(
      new ApiError('invalid_input', '"XYZ" is not a known currency.'),
    )
    render(<AccountsSettings />)
    await screen.findByRole('button', { name: 'Add' })

    await user.type(screen.getByLabelText('New account name'), 'Vacation fund')
    await user.type(screen.getByLabelText('Currency'), 'xyz')
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    // Creating an account is a user-triggered action
    // (docs/design-system.md's "Toasts vs. inline messages"), so its
    // failure is reported via a toast, not inline.
    await waitFor(() =>
      expect(mockedToastError).toHaveBeenCalledWith(
        '"XYZ" is not a known currency.',
      ),
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
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

  it('leaves the account in place and reports a toast (not inline) when archive fails', async () => {
    mockedListAccounts.mockResolvedValue([
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
    mockedArchiveAccount.mockRejectedValue(
      new ApiError('internal', 'Couldn’t reach the server. Try again.'),
    )
    render(<AccountsSettings />)

    const archiveButton = await screen.findByRole('button', {
      name: 'Archive',
    })
    fireEvent.click(archiveButton)

    await waitFor(() => expect(mockedToastPromise).toHaveBeenCalled())
    // Archiving an account is a user-triggered action
    // (docs/design-system.md's "Toasts vs. inline messages"), so its
    // failure is reported via toast.promise's `error` option, not
    // inline — the account stays in the list and no role="alert"
    // appears.
    const [, options] = mockedToastPromise.mock.calls[0]
    const toastError = options?.error as (err: unknown) => string
    expect(
      toastError(
        new ApiError('internal', 'Couldn’t reach the server. Try again.'),
      ),
    ).toBe('Couldn’t reach the server. Try again.')

    await waitFor(() =>
      expect(screen.getByText('Checking')).toBeInTheDocument(),
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
