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
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { TransactionDialogProvider } from '@/components/TransactionDialog'
import { useTransactionDialog } from '@/hooks/use-transaction-dialog'

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
    getReportingCurrency: vi.fn(),
    getFxRate: vi.fn(),
    fetchFxRates: vi.fn(),
  }
})

import {
  ApiError,
  deleteTransaction,
  fetchFxRates,
  getFxRate,
  getReportingCurrency,
  listAccounts,
  listCategories,
  listTransactions,
  recordOutflow,
  updateTransaction,
  type Account,
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
const mockedGetReportingCurrency = vi.mocked(getReportingCurrency)
const mockedGetFxRate = vi.mocked(getFxRate)
const mockedFetchFxRates = vi.mocked(fetchFxRates)

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
    mockedGetReportingCurrency.mockReset()
    mockedGetFxRate.mockReset()
    mockedFetchFxRates.mockReset()
    // Matching Balances.test.tsx's own default: no reporting currency set,
    // so the per-row conversion machinery (issue #145) stays fully inert
    // unless a test opts in — a single-currency ledger must never meet
    // any of it (docs/ux-principles.md §4).
    mockedGetReportingCurrency.mockResolvedValue({
      currency: '',
      is_set: false,
    })
    stubLookups()
  })

  it('renders an empty state with no transactions', async () => {
    mockedListTransactions.mockResolvedValue({ data: [] })
    renderPage()

    expect(await screen.findByText('No transactions yet')).toBeInTheDocument()
  })

  // Regression test, from direct user feedback: the page's own root
  // container was missing flex-1 (present on Balances.tsx's equivalent
  // root div), so <Empty>'s own flex-1 had no extra vertical space in
  // its flex-col parent to expand into and center within — it just sat
  // at content height under the header instead of centered on the page.
  it('gives the page container flex-1 so the empty state centers like Balances does', async () => {
    mockedListTransactions.mockResolvedValue({ data: [] })
    const { container } = renderPage()
    await screen.findByText('No transactions yet')

    expect(container.querySelector(':scope > div')).toHaveClass('flex-1')
  })

  it('refreshes when a transaction is created by something else entirely (e.g. the nav)', async () => {
    const user = userEvent.setup()
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
    // The Category field is a Combobox (issue #106), not a native
    // <select> — pick it via the real interaction, same as
    // TransactionDialog.test.tsx's own pickCategory helper. Its popover
    // content renders in its own portal, outside `dialog`'s own DOM
    // subtree, so the option is queried from the whole document rather
    // than scoped `within(dialog)`.
    await user.click(within(dialog).getByRole('combobox', { name: 'Category' }))
    await user.click(await screen.findByRole('option', { name: 'Food' }))
    await waitFor(() =>
      expect(
        within(dialog).getByRole('combobox', { name: 'Account' }),
      ).toHaveTextContent('Checking'),
    )
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
    expect(
      await screen.findByRole('combobox', { name: 'Account' }),
    ).toHaveTextContent('Checking')
  })

  it('shows a nested category’s full ancestry path in the filter (issue #106)', async () => {
    mockedListCategories.mockResolvedValue([
      {
        id: 'c1',
        name: 'Food',
        type: 'expense',
        sort_order: 0,
        archived: false,
      },
      {
        id: 'c2',
        name: 'Groceries',
        type: 'expense',
        sort_order: 0,
        archived: false,
        parent_id: 'c1',
      },
    ])
    mockedListTransactions.mockResolvedValue({ data: [] })
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('No transactions yet')

    fireEvent.click(screen.getByRole('button', { name: /Filters/ }))
    await user.click(await screen.findByRole('combobox', { name: 'Category' }))

    expect(
      await screen.findByRole('option', { name: 'Food' }),
    ).toBeInTheDocument()
    // The child's own list label is just its name ("Groceries") — the
    // hierarchy shows as indentation there, not a repeated path — but
    // its full "Food > Groceries" ancestry is still findable by typing
    // either name, per Combobox's own path-based search.
    await user.type(
      screen.getByRole('combobox', { name: /search/i }),
      'Food > Groceries',
    )
    expect(
      await screen.findByRole('option', { name: 'Groceries' }),
    ).toBeInTheDocument()
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
    expect(
      await screen.findByRole('combobox', { name: 'Category' }),
    ).toHaveTextContent('Food')
  })

  it('applies a filter and re-fetches with it', async () => {
    const user = userEvent.setup()
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: /Filters/ }))
    await user.click(screen.getByRole('combobox', { name: 'Account' }))
    await user.click(await screen.findByRole('option', { name: 'Checking' }))
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

  it('filters by a picked From date via the Radix date picker (issue #104)', async () => {
    const user = userEvent.setup()
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: /Filters/ }))
    await user.click(screen.getByRole('button', { name: /From/ }))
    await user.click(await screen.findByRole('button', { name: /15th, 2026/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() =>
      expect(mockedListTransactions).toHaveBeenLastCalledWith(
        expect.objectContaining({
          from: expect.stringMatching(/^2026-\d{2}-15$/),
        }),
      ),
    )
  })

  it('title-cases the type filter options, matching the account/category type dropdowns', async () => {
    const user = userEvent.setup()
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: /Filters/ }))
    await user.click(screen.getByRole('combobox', { name: 'Type' }))

    expect(
      await screen.findByRole('option', { name: 'Spend' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Receive' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Move' })).toBeInTheDocument()
  })

  it('clearing filters resets the form so a later Apply is not stale', async () => {
    const user = userEvent.setup()
    mockedListTransactions.mockResolvedValue({ data: [groceries] })
    renderPage()
    await screen.findByText('Groceries')

    fireEvent.click(screen.getByRole('button', { name: /Filters/ }))
    await user.click(screen.getByRole('combobox', { name: 'Account' }))
    await user.click(await screen.findByRole('option', { name: 'Checking' }))
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
    // It was the form still displaying "Checking" afterwards, so a second
    // Apply with no further changes would silently resubmit it.
    expect(screen.getByRole('combobox', { name: 'Account' })).toHaveTextContent(
      'Any',
    )

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

  // Issue #163: transactionView now reports a cross-currency transfer's
  // from-leg as amount/currency and its to-leg as to_amount/to_currency
  // (dto.go's transactionView doc comment) — TransactionDialog's edit-mode
  // pre-fill must read the from-leg into "Amount" and the to-leg into the
  // destination-amount field, not assume "Amount" is safe to resubmit
  // as-is. Before the fix, this transfer's GET-shaped amount was the
  // to-leg (8000.00 INR) with no to_amount at all, so the "Amount" field
  // would have pre-filled 8000.00 and a no-op Save would have silently
  // turned a 100 USD -> 8000 INR transfer into an 8000 USD -> 8000 INR one.
  it('pre-fills a cross-currency move’s from-leg and to-leg separately, and a no-op save preserves both', async () => {
    const inrSavings: Account = {
      id: 'a3',
      name: 'INR Savings',
      type: 'bank',
      currency: 'INR',
      opening_balance: '0.00',
      sort_order: 2,
      archived: false,
    }
    const usdChecking: Account = {
      id: 'a1',
      name: 'Checking',
      type: 'bank',
      currency: 'USD',
      opening_balance: '0.00',
      sort_order: 0,
      archived: false,
    }
    mockedListAccounts.mockResolvedValue([usdChecking, inrSavings])
    const move: Transaction = {
      id: 't2',
      type: 'transfer',
      date: '2026-08-13',
      description: 'Transfer from Checking to INR Savings',
      from_account_id: 'a1',
      to_account_id: 'a3',
      amount: '100.00',
      currency: 'USD',
      to_amount: '8000.00',
      to_currency: 'INR',
    }
    mockedListTransactions.mockResolvedValue({ data: [move] })
    mockedUpdateTransaction.mockResolvedValue(move)
    renderPage()
    await screen.findByText('Transfer from Checking to INR Savings')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    const dialog = await screen.findByRole('dialog', {
      name: 'Edit transaction',
    })
    expect(within(dialog).getByLabelText('Amount')).toHaveValue('100.00')
    expect(
      await within(dialog).findByLabelText('Amount received (INR)'),
    ).toHaveValue('8000.00')

    fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(mockedUpdateTransaction).toHaveBeenCalledWith(
        't2',
        expect.objectContaining({
          amount: '100.00',
          to_amount: '8000.00',
          from_account: 'a1',
          to_account: 'a3',
        }),
      ),
    )
  })

  // Issue #145: per-row reporting-currency equivalent, the backfill
  // popover, and the booked_date-vs-now substitution regression. Every
  // test in this block sets a reporting currency (mocked getReportingCurrency)
  // so the per-row machinery is actually active — the beforeEach default
  // above leaves it unset/inert, matching a single-currency ledger.
  describe('foreign-currency conversion (issue #145)', () => {
    const souvenir: Transaction = {
      id: 't2',
      type: 'outflow',
      date: '2026-01-10',
      description: 'Souvenir',
      account_id: 'a1',
      category_id: 'c1',
      amount: '4200.00',
      currency: 'INR',
    }

    it("shows a foreign-currency transaction's reporting-currency equivalent, expandable to its provenance", async () => {
      const user = userEvent.setup()
      mockedGetReportingCurrency.mockResolvedValue({
        currency: 'USD',
        is_set: true,
      })
      mockedListTransactions.mockResolvedValue({ data: [souvenir] })
      mockedGetFxRate.mockResolvedValue({
        from: 'INR',
        to: 'USD',
        rate: '0.012',
        rate_date: '2026-01-10',
        rate_source: 'frankfurter',
        stale: false,
        policy: 'transaction_date',
        amount: '4200.00',
        // Already carries its own currency code, same wire shape
        // internal/surface/http/fx.go's fxRateViewFrom uses — not a bare
        // number the UI appends a currency onto itself.
        converted: '50.40 USD',
      })

      renderPage()
      await screen.findByText('Souvenir')

      const approx = await screen.findByText('≈ 50.40 USD')
      await user.click(approx)
      expect(
        screen.getByText(/1 INR = 0.012 USD.*frankfurter/),
      ).toBeInTheDocument()
    })

    // The actual bug shape issue #145 calls out: the lookup must use the
    // transaction's own booked date, never "today" — modeled on how
    // ADR-0012 requires a real test for its own response-date rule
    // rather than trusting a comment. `souvenir` is booked 2026-01-10;
    // "today" is faked to a completely different date below, so this only
    // passes if the code sends the transaction's own date and not the
    // clock's.
    it("evaluates a row's conversion against its own booked date, not today's date", async () => {
      // Only Date is faked (not timers) so testing-library's own
      // real-timer-based polling (waitFor) keeps working — this just
      // pins "today" to a date far from the transaction's own booked
      // date, so nothing here could pass by coincidence.
      vi.useFakeTimers({ toFake: ['Date'] })
      vi.setSystemTime(new Date('2026-09-06T12:00:00Z'))
      try {
        mockedGetReportingCurrency.mockResolvedValue({
          currency: 'USD',
          is_set: true,
        })
        mockedListTransactions.mockResolvedValue({ data: [souvenir] })
        mockedGetFxRate.mockResolvedValue({
          from: 'INR',
          to: 'USD',
          rate: '0.012',
          rate_date: '2026-01-10',
          rate_source: 'frankfurter',
          stale: false,
          policy: 'transaction_date',
          amount: '4200.00',
          converted: '50.40 USD',
        })

        renderPage()
        await waitFor(() =>
          expect(mockedGetFxRate).toHaveBeenCalledWith({
            from: 'INR',
            to: 'USD',
            amount: '4200.00',
            policy: 'transaction_date',
            transactionDate: '2026-01-10',
          }),
        )
        // Belt and braces: the call must not have used "today" in either
        // role a regression could substitute it into.
        expect(mockedGetFxRate).not.toHaveBeenCalledWith(
          expect.objectContaining({ transactionDate: '2026-09-06' }),
        )
        expect(mockedGetFxRate).not.toHaveBeenCalledWith(
          expect.objectContaining({ policy: 'current' }),
        )
      } finally {
        vi.useRealTimers()
      }
    })

    it('shows an unconverted row explicitly instead of dropping or blanking it', async () => {
      mockedGetReportingCurrency.mockResolvedValue({
        currency: 'USD',
        is_set: true,
      })
      mockedListTransactions.mockResolvedValue({ data: [souvenir] })
      mockedGetFxRate.mockRejectedValue(
        new ApiError(
          'not_found',
          'No INR/USD rate available for 2026-01-10 within 7 day(s).',
        ),
      )

      renderPage()
      await screen.findByText('Souvenir')

      expect(
        await screen.findByText(
          'Not converted — No INR/USD rate available for 2026-01-10 within 7 day(s).',
        ),
      ).toBeInTheDocument()
    })

    it('backfills rates over a currency multi-select and date range, then resolves rows in place', async () => {
      const user = userEvent.setup()
      const eurTx: Transaction = {
        id: 't3',
        type: 'outflow',
        date: '2026-03-05',
        description: 'Hotel',
        account_id: 'a1',
        category_id: 'c1',
        amount: '80.00',
        currency: 'EUR',
      }
      mockedGetReportingCurrency.mockResolvedValue({
        currency: 'USD',
        is_set: true,
      })
      mockedListTransactions.mockResolvedValue({ data: [souvenir, eurTx] })
      // Initial page load: souvenir (INR) resolves stale, eurTx (EUR) has
      // no stored rate at all — both should end up pre-checked in the
      // backfill popover, and both dates should bound its default range.
      mockedGetFxRate
        .mockResolvedValueOnce({
          from: 'INR',
          to: 'USD',
          rate: '0.011',
          rate_date: '2026-01-03',
          rate_source: 'frankfurter',
          stale: true,
          policy: 'transaction_date',
          amount: '4200.00',
          converted: '46.20 USD',
        })
        .mockRejectedValueOnce(
          new ApiError(
            'not_found',
            'No EUR/USD rate available for 2026-03-05 within 7 day(s).',
          ),
        )
        // Post-backfill re-read: both rows now resolve cleanly.
        .mockResolvedValue({
          from: 'EUR',
          to: 'USD',
          rate: '1.08',
          rate_date: '2026-03-05',
          rate_source: 'frankfurter',
          stale: false,
          policy: 'transaction_date',
          amount: '80.00',
          converted: '86.40 USD',
        })
      mockedFetchFxRates.mockResolvedValue({
        reporting_currency: 'USD',
        fetched: [
          {
            pair: 'INR/USD',
            rate: '0.0115',
            date: '2026-01-10',
            source: 'frankfurter',
          },
          {
            pair: 'EUR/USD',
            rate: '1.08',
            date: '2026-03-05',
            source: 'frankfurter',
          },
        ],
      })

      renderPage()
      await screen.findByText('Souvenir')
      await screen.findByText('Hotel')
      // Both rows start out flagged (stale amount for INR, explicit
      // "Not converted" for EUR).
      expect(await screen.findByText('stale')).toBeInTheDocument()
      expect(
        await screen.findByText(/Not converted — No EUR\/USD/),
      ).toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: /Backfill rates/ }))
      // Currency multi-select — the key difference from #139's own
      // popover, which never needs one since it always targets "today".
      expect(await screen.findByRole('checkbox', { name: 'INR' })).toBeChecked()
      expect(screen.getByRole('checkbox', { name: 'EUR' })).toBeChecked()
      // And a date range — #139's popover never has one at all. The
      // trigger buttons' *accessible name* is their associated <label>
      // ("From"/"To", same as the filter form's own date pickers above),
      // not the picked date — that's rendered as the button's visible
      // text instead (date-fns' 'PP' format, e.g. "Jan 10, 2026").
      expect(screen.getByText(/10, 2026/)).toBeInTheDocument()
      expect(screen.getByText(/5, 2026/)).toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: 'Backfill' }))

      expect(mockedFetchFxRates).toHaveBeenCalledWith(['INR', 'EUR'], {
        from: '2026-01-10',
        to: '2026-03-05',
      })
      // Re-read the visible page (matching #139's own re-read-after-
      // refresh): the previously stale/unconverted rows resolve in place
      // rather than needing a manual reload.
      await waitFor(() => {
        expect(screen.queryByText('stale')).not.toBeInTheDocument()
        expect(screen.queryByText(/Not converted/)).not.toBeInTheDocument()
      })
    })
  })
})
