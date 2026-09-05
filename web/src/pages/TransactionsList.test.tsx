// Component tests for the transaction list screen (issue #61). @/lib/api
// is mocked throughout — these exercise the page's own behaviour (render,
// filter, edit, delete), not the real fetch/CSRF plumbing api.test.ts
// already covers in isolation.
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    listTransactions: vi.fn(),
    listAccounts: vi.fn(),
    listCategories: vi.fn(),
    updateTransaction: vi.fn(),
    deleteTransaction: vi.fn(),
  }
})

import {
  ApiError,
  deleteTransaction,
  listAccounts,
  listCategories,
  listTransactions,
  updateTransaction,
  type Transaction,
} from '@/lib/api'
import { TransactionsList } from './TransactionsList'

const mockedListTransactions = vi.mocked(listTransactions)
const mockedListAccounts = vi.mocked(listAccounts)
const mockedListCategories = vi.mocked(listCategories)
const mockedUpdateTransaction = vi.mocked(updateTransaction)
const mockedDeleteTransaction = vi.mocked(deleteTransaction)

// The empty state links to /transactions/new (Empty's "Record a
// transaction" CTA), which needs a router context.
function renderPage() {
  return render(
    <MemoryRouter>
      <TransactionsList />
    </MemoryRouter>,
  )
}

const groceries: Transaction = {
  id: 't1',
  type: 'outflow',
  date: '2026-09-01',
  description: 'Groceries',
  account_id: 'a1',
  category_id: 'c1',
  amount: '42.50',
  currency: 'USD',
}

function stubLookups() {
  mockedListAccounts.mockResolvedValue([
    {
      id: 'a1',
      name: 'Checking',
      type: 'bank',
      currency: 'USD',
      opening_balance: '0.00',
      sort_order: 0,
      archived: false,
    },
    {
      id: 'a2',
      name: 'Savings',
      type: 'bank',
      currency: 'USD',
      opening_balance: '0.00',
      sort_order: 1,
      archived: false,
    },
  ])
  mockedListCategories.mockResolvedValue([
    {
      id: 'c1',
      name: 'Food',
      type: 'expense',
      sort_order: 0,
      archived: false,
    },
  ])
}

describe('TransactionsList', () => {
  beforeEach(() => {
    mockedListTransactions.mockReset()
    mockedListAccounts.mockReset()
    mockedListCategories.mockReset()
    mockedUpdateTransaction.mockReset()
    mockedDeleteTransaction.mockReset()
    stubLookups()
  })

  it('renders an empty state with no transactions', async () => {
    mockedListTransactions.mockResolvedValue({ data: [] })
    renderPage()

    expect(await screen.findByText('No transactions yet')).toBeInTheDocument()
  })

  it('renders a transaction row with its account and category', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    renderPage()

    expect(await screen.findByText('Groceries')).toBeInTheDocument()
    expect(screen.getByText(/spend/)).toBeInTheDocument()
    expect(screen.getByText(/Checking/)).toBeInTheDocument()
    expect(screen.getByText(/Food/)).toBeInTheDocument()
    expect(screen.getByText('−42.50 USD')).toBeInTheDocument()
  })

  it('applies a filter and re-fetches with it', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: /Filters/ }))
    fireEvent.change(await screen.findByLabelText('Account'), {
      target: { value: 'a1' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() =>
      expect(mockedListTransactions).toHaveBeenLastCalledWith({
        account: 'a1',
        category: undefined,
        type: undefined,
        from: undefined,
        to: undefined,
      }),
    )
  })

  it('title-cases the type filter options, matching the account/category type dropdowns', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: /Filters/ }))
    const typeSelect = await screen.findByLabelText('Type')

    expect(
      within(typeSelect).getByRole('option', { name: 'Spend' }),
    ).toBeInTheDocument()
    expect(
      within(typeSelect).getByRole('option', { name: 'Receive' }),
    ).toBeInTheDocument()
    expect(
      within(typeSelect).getByRole('option', { name: 'Move' }),
    ).toBeInTheDocument()
  })

  it('clearing filters resets the form so a later Apply is not stale', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: /Filters/ }))
    fireEvent.change(await screen.findByLabelText('Account'), {
      target: { value: 'a1' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))
    await waitFor(() =>
      expect(mockedListTransactions).toHaveBeenLastCalledWith(
        expect.objectContaining({ account: 'a1' }),
      ),
    )

    fireEvent.click(screen.getByRole('button', { name: 'Clear' }))
    await waitFor(() =>
      expect(mockedListTransactions).toHaveBeenLastCalledWith({
        account: undefined,
        category: undefined,
        type: undefined,
        from: undefined,
        to: undefined,
      }),
    )
    // The bug wasn't the fetch — clearFilters() always fetched unfiltered.
    // It was the form still displaying "a1" afterwards, so a second Apply
    // with no further changes would silently resubmit it.
    expect(screen.getByLabelText('Account')).toHaveValue('')

    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))
    await waitFor(() =>
      expect(mockedListTransactions).toHaveBeenLastCalledWith({
        account: undefined,
        category: undefined,
        type: undefined,
        from: undefined,
        to: undefined,
      }),
    )
  })

  it('deletes a transaction and removes it from the list', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    mockedDeleteTransaction.mockResolvedValue(groceries)
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    await waitFor(() =>
      expect(screen.queryByText('Groceries')).not.toBeInTheDocument(),
    )
    expect(mockedDeleteTransaction).toHaveBeenCalledWith('t1')
  })

  it('edits a transaction, sending the full replacement body', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    mockedUpdateTransaction.mockResolvedValue({ ...groceries, amount: '50.00' })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    const form = screen.getByRole('form', { name: 'Edit Groceries' })
    fireEvent.change(within(form).getByLabelText('Amount'), {
      target: { value: '50.00' },
    })
    fireEvent.click(within(form).getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(mockedUpdateTransaction).toHaveBeenCalledWith('t1', {
        amount: '50.00',
        description: 'Groceries',
        date: '2026-09-01',
        notes: undefined,
        account: 'a1',
        category: 'c1',
      }),
    )
    expect(await screen.findByText('−50.00 USD')).toBeInTheDocument()
  })

  it('preserves the edit form and shows an error when saving fails', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    mockedUpdateTransaction.mockRejectedValue(
      new ApiError('invalid_input', 'That amount doesn’t look right.'),
    )
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    const form = screen.getByRole('form', { name: 'Edit Groceries' })
    fireEvent.change(within(form).getByLabelText('Amount'), {
      target: { value: 'nonsense' },
    })
    fireEvent.click(within(form).getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'That amount doesn’t look right.',
    )
    // Partial input is preserved (ux-principles.md §5) — the form is
    // still open with what was typed, not discarded back to the row.
    expect(within(form).getByLabelText('Amount')).toHaveValue('nonsense')
  })
})
