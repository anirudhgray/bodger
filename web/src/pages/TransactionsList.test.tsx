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

import {
  TransactionDialogProvider,
  useTransactionDialog,
} from '@/components/TransactionDialog'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    listTransactions: vi.fn(),
    listAccounts: vi.fn(),
    listCategories: vi.fn(),
    updateTransaction: vi.fn(),
    deleteTransaction: vi.fn(),
    recordOutflow: vi.fn(),
  }
})

import {
  ApiError,
  deleteTransaction,
  listAccounts,
  listCategories,
  listTransactions,
  recordOutflow,
  updateTransaction,
  type Transaction,
  type TransactionListFilter,
} from '@/lib/api'
import { TransactionsList } from './TransactionsList'

const mockedListTransactions = vi.mocked(listTransactions)
const mockedListAccounts = vi.mocked(listAccounts)
const mockedListCategories = vi.mocked(listCategories)
const mockedUpdateTransaction = vi.mocked(updateTransaction)
const mockedDeleteTransaction = vi.mocked(deleteTransaction)
const mockedRecordOutflow = vi.mocked(recordOutflow)

// The empty state's "Record a transaction" CTA needs a router context;
// editing/creating a transaction (both open TransactionDialog) needs
// TransactionDialogProvider, same as AppLayout provides for real.
function renderPage() {
  return render(
    <MemoryRouter>
      <TransactionDialogProvider>
        <TransactionsList />
      </TransactionDialogProvider>
    </MemoryRouter>,
  )
}

// Stands in for the nav's global Add button — something that opens the
// same shared dialog but has no reference to this screen's own load() or
// setTransactions, the way the real nav button doesn't either.
function ExternalAddTrigger() {
  const { openCreate } = useTransactionDialog()
  return (
    <button type="button" onClick={() => openCreate()}>
      External add
    </button>
  )
}

// Stands in for arriving via cross-navigation (issue #89) — e.g. clicking
// an account on Balances — which hands the initial filter in through
// router state, not a query string.
function renderPageWithInitialFilter(filter: TransactionListFilter) {
  return render(
    <MemoryRouter
      initialEntries={[{ pathname: '/transactions', state: { filter } }]}
    >
      <TransactionDialogProvider>
        <TransactionsList />
      </TransactionDialogProvider>
    </MemoryRouter>,
  )
}

function renderPageWithExternalTrigger() {
  return render(
    <MemoryRouter>
      <TransactionDialogProvider>
        <ExternalAddTrigger />
        <TransactionsList />
      </TransactionDialogProvider>
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
    mockedRecordOutflow.mockReset()
    stubLookups()
  })

  it('renders an empty state with no transactions', async () => {
    mockedListTransactions.mockResolvedValue({ data: [] })
    renderPage()

    expect(await screen.findByText('No transactions yet')).toBeInTheDocument()
  })

  it('refreshes when a transaction is created by something else entirely (e.g. the nav)', async () => {
    mockedListTransactions
      .mockResolvedValueOnce({ data: [] })
      .mockResolvedValueOnce({ data: [groceries] })
    mockedRecordOutflow.mockResolvedValue(groceries)
    renderPageWithExternalTrigger()

    await screen.findByText('No transactions yet')

    fireEvent.click(screen.getByText('External add'))
    const dialog = await screen.findByRole('dialog', {
      name: 'Add transaction',
    })
    fireEvent.change(within(dialog).getByLabelText('Amount'), {
      target: { value: '42.50' },
    })
    fireEvent.change(within(dialog).getByLabelText('Category'), {
      target: { value: 'c1' },
    })
    fireEvent.click(
      within(dialog).getByRole('button', { name: 'Record spend' }),
    )

    // This is the actual bug: without TransactionsList subscribing to
    // saves from *any* source, this screen — which had no part in
    // opening this particular dialog — never re-fetches, and the new
    // transaction silently doesn't appear until a manual reload.
    expect(await screen.findByText('Groceries')).toBeInTheDocument()
    expect(mockedListTransactions).toHaveBeenCalledTimes(2)
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

  it('loads with an initial filter handed in via cross-navigation and shows it applied', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    renderPageWithInitialFilter({ account: 'a1' })

    await waitFor(() =>
      expect(mockedListTransactions).toHaveBeenCalledWith({ account: 'a1' }),
    )
    expect(
      screen.getByRole('button', { name: /Hide filters/ }),
    ).toBeInTheDocument()
    expect(await screen.findByLabelText('Account')).toHaveValue('a1')
  })

  it('filters by category when a transaction row’s category is clicked', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: 'Food' }))

    await waitFor(() =>
      expect(mockedListTransactions).toHaveBeenLastCalledWith({
        category: 'c1',
      }),
    )
    expect(await screen.findByLabelText('Category')).toHaveValue('c1')
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

  it('edits a transaction via the dialog, sending the full replacement body', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    mockedUpdateTransaction.mockResolvedValue({ ...groceries, amount: '50.00' })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog', {
      name: 'Edit transaction',
    })
    fireEvent.change(within(dialog).getByLabelText('Amount'), {
      target: { value: '50.00' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(mockedUpdateTransaction).toHaveBeenCalledWith('t1', {
        amount: '50.00',
        description: 'Groceries',
        date: '2026-09-01',
        notes: undefined,
        tags: undefined,
        account: 'a1',
        category: 'c1',
      }),
    )
    expect(await screen.findByText('−50.00 USD')).toBeInTheDocument()
  })

  it('preserves notes and tags on edit, instead of silently clearing them', async () => {
    const withNotesAndTags: Transaction = {
      ...groceries,
      notes: 'Weekly shop',
      tags: ['essential', 'weekly'],
    }
    mockedListTransactions.mockResolvedValue({ data: [withNotesAndTags] })
    mockedUpdateTransaction.mockResolvedValue({
      ...withNotesAndTags,
      amount: '50.00',
    })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog', {
      name: 'Edit transaction',
    })
    // Notes/tags should already be visible and pre-filled, not hidden
    // behind a disclosure toggle — this is what the old in-row edit form
    // couldn't show at all, and was silently wiping on every save.
    expect(within(dialog).getByLabelText('Notes')).toHaveValue('Weekly shop')
    expect(within(dialog).getByLabelText('Tags')).toHaveValue(
      'essential, weekly',
    )

    fireEvent.change(within(dialog).getByLabelText('Amount'), {
      target: { value: '50.00' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(mockedUpdateTransaction).toHaveBeenCalledWith(
        't1',
        expect.objectContaining({
          notes: 'Weekly shop',
          tags: ['essential', 'weekly'],
        }),
      ),
    )
  })

  it('rejects non-numeric characters in the edit dialog amount field', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog', {
      name: 'Edit transaction',
    })
    const amountField = within(dialog).getByLabelText('Amount')

    fireEvent.change(amountField, { target: { value: 'soemthing' } })
    expect(amountField).toHaveValue('')

    fireEvent.change(amountField, { target: { value: '$50.00' } })
    expect(amountField).toHaveValue('50.00')
  })

  it('preserves the edit dialog and shows an error when saving fails', async () => {
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    mockedUpdateTransaction.mockRejectedValue(
      new ApiError('invalid_input', 'That amount doesn’t look right.'),
    )
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog', {
      name: 'Edit transaction',
    })
    // A well-formed number, not garbage text — the amount field's own
    // client-side sanitization (a separate concern this suite tests on
    // its own) means garbage can no longer reach a submit at all. This
    // is testing the server rejecting a validly-typed value on rules the
    // client doesn't know about (the mock controls that, independent of
    // what's actually typed here).
    fireEvent.change(within(dialog).getByLabelText('Amount'), {
      target: { value: '999999.99' },
    })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'That amount doesn’t look right.',
    )
    // Partial input is preserved (ux-principles.md §5) — the dialog is
    // still open with what was typed, not discarded.
    expect(within(dialog).getByLabelText('Amount')).toHaveValue('999999.99')
  })
})
