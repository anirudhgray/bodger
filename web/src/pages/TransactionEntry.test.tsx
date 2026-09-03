// Component tests for fast transaction entry (issue #60): the three-field
// happy path ux-principles.md §3 asks be checkable, and a validation
// failure that preserves what was typed (§5).
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    listAccounts: vi.fn(),
    listCategories: vi.fn(),
    recordOutflow: vi.fn(),
    recordInflow: vi.fn(),
    recordTransfer: vi.fn(),
  }
})

import {
  listAccounts,
  listCategories,
  recordOutflow,
  ApiError,
} from '@/lib/api'
import { TransactionEntry } from './TransactionEntry'

const mockedListAccounts = vi.mocked(listAccounts)
const mockedListCategories = vi.mocked(listCategories)
const mockedRecordOutflow = vi.mocked(recordOutflow)

const account = {
  id: 'acc-1',
  name: 'HDFC Savings',
  type: 'checking',
  currency: 'INR',
  archived: false,
}
const groceries = {
  id: 'cat-1',
  name: 'Groceries',
  type: 'expense' as const,
  archived: false,
}

beforeEach(() => {
  mockedListAccounts.mockReset().mockResolvedValue([account])
  mockedListCategories.mockReset().mockResolvedValue([groceries])
  mockedRecordOutflow.mockReset()
  localStorage.clear()
})

describe('TransactionEntry', () => {
  it('records an expense with just amount, category, and account (the one account preselected)', async () => {
    mockedRecordOutflow.mockResolvedValue({})
    render(<TransactionEntry />)

    await screen.findByText('HDFC Savings')

    fireEvent.change(screen.getByLabelText('Amount'), {
      target: { value: '800' },
    })
    fireEvent.change(screen.getByLabelText('Category'), {
      target: { value: 'cat-1' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Record spend' }))

    await waitFor(() => expect(mockedRecordOutflow).toHaveBeenCalledTimes(1))
    expect(mockedRecordOutflow).toHaveBeenCalledWith(
      expect.objectContaining({
        account: 'acc-1',
        category: 'cat-1',
        amount: '800',
      }),
    )
    expect(await screen.findByRole('status')).toHaveTextContent('Recorded.')
  })

  it('preserves what was typed when the API rejects the submission', async () => {
    mockedRecordOutflow.mockRejectedValue(
      new ApiError(
        'invalid_input',
        "Couldn't find a category called 'cat-1'.",
        'category',
      ),
    )
    render(<TransactionEntry />)

    await screen.findByText('HDFC Savings')

    fireEvent.change(screen.getByLabelText('Amount'), {
      target: { value: '800' },
    })
    fireEvent.change(screen.getByLabelText('Category'), {
      target: { value: 'cat-1' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Record spend' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      "Couldn't find a category called 'cat-1'.",
    )
    expect(screen.getByLabelText('Amount')).toHaveValue('800')
    expect(screen.getByLabelText('Category')).toHaveValue('cat-1')
  })

  it('keeps the submit button disabled until amount and category are filled in', async () => {
    render(<TransactionEntry />)
    await screen.findByText('HDFC Savings')

    expect(screen.getByRole('button', { name: 'Record spend' })).toBeDisabled()
  })

  it('switches to a Move form with from/to accounts and no category field', async () => {
    mockedListAccounts.mockResolvedValue([
      account,
      { ...account, id: 'acc-2', name: 'Checking' },
    ])
    render(<TransactionEntry />)

    await screen.findByRole('button', { name: 'Move' })
    fireEvent.click(screen.getByRole('button', { name: 'Move' }))

    expect(screen.queryByLabelText('Category')).not.toBeInTheDocument()
    expect(screen.getByLabelText('From account')).toBeInTheDocument()
    expect(screen.getByLabelText('To account')).toBeInTheDocument()
  })
})
