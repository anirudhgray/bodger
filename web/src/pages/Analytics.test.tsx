// Component tests for the analytics screen (issue #189). lib/api's four
// analytics getters plus getReportingCurrency/listAccounts/fetchFxRates
// are mocked so these exercise only the screen's own rendering: loading,
// real chart data, an empty period, a server error, and the unconverted-
// postings banner with its refresh popover.
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    getCategoryBreakdown: vi.fn(),
    getCashFlow: vi.fn(),
    getTrends: vi.fn(),
    getSavingsRate: vi.fn(),
    getTopTransactions: vi.fn(),
    getAverageTransactionSize: vi.fn(),
    getCategoryTrends: vi.fn(),
    getReportingCurrency: vi.fn(),
    listAccounts: vi.fn(),
    fetchFxRates: vi.fn(),
  }
})

import {
  ApiError,
  getAverageTransactionSize,
  getCashFlow,
  getCategoryBreakdown,
  getCategoryTrends,
  getReportingCurrency,
  getSavingsRate,
  getTopTransactions,
  getTrends,
  listAccounts,
} from '@/lib/api'
import { AnalyticsPage } from './Analytics'

const mockedGetCategoryBreakdown = vi.mocked(getCategoryBreakdown)
const mockedGetCashFlow = vi.mocked(getCashFlow)
const mockedGetTrends = vi.mocked(getTrends)
const mockedGetSavingsRate = vi.mocked(getSavingsRate)
const mockedGetTopTransactions = vi.mocked(getTopTransactions)
const mockedGetAverageTransactionSize = vi.mocked(getAverageTransactionSize)
const mockedGetCategoryTrends = vi.mocked(getCategoryTrends)
const mockedGetReportingCurrency = vi.mocked(getReportingCurrency)
const mockedListAccounts = vi.mocked(listAccounts)

// emptyCategoryTrends/emptyAverageSize/emptyTopTransactions (issue #196):
// shared "nothing to report" defaults for the three additional stats,
// applied in beforeEach below so every existing test written before
// these three existed keeps working without having to mock them
// individually — the same role mockedGetReportingCurrency/
// mockedListAccounts' own beforeEach defaults already play.
const emptyCategoryTrends = {
  currency: 'USD',
  current_from: '2026-09-01',
  current_to: '2026-09-30',
  previous_from: '2026-08-01',
  previous_to: '2026-08-31',
  rows: [],
}

function renderPage() {
  return render(
    <MemoryRouter>
      <AnalyticsPage />
    </MemoryRouter>,
  )
}

const emptyTrends = {
  currency: 'USD',
  current: {
    from: '2026-09-01',
    to: '2026-09-30',
    inflow: '0',
    outflow: '0',
    net: '0',
  },
  previous: {
    from: '2026-08-01',
    to: '2026-08-31',
    inflow: '0',
    outflow: '0',
    net: '0',
  },
}

describe('AnalyticsPage', () => {
  beforeEach(() => {
    mockedGetCategoryBreakdown.mockReset()
    mockedGetCashFlow.mockReset()
    mockedGetTrends.mockReset()
    mockedGetSavingsRate.mockReset()
    mockedGetTopTransactions.mockReset()
    mockedGetAverageTransactionSize.mockReset()
    mockedGetCategoryTrends.mockReset()
    mockedGetReportingCurrency.mockReset()
    mockedListAccounts.mockReset()
    mockedGetReportingCurrency.mockResolvedValue({
      currency: '',
      is_set: false,
      effectiveCurrency: 'USD',
    })
    mockedListAccounts.mockResolvedValue([])
    mockedGetTopTransactions.mockResolvedValue({ currency: 'USD', rows: [] })
    mockedGetAverageTransactionSize.mockResolvedValue({
      currency: 'USD',
      overall: { category: 'Uncategorized', count: 0, average: '0.00' },
      by_category: [],
    })
    mockedGetCategoryTrends.mockResolvedValue(emptyCategoryTrends)
  })

  it('renders category, cash-flow, trends, and savings-rate data from the API as-is', async () => {
    mockedGetCategoryBreakdown.mockResolvedValue({
      currency: 'USD',
      rows: [
        {
          category: 'Food',
          spending: '150.00',
          income: '0.00',
          net: '-150.00',
        },
      ],
    })
    mockedGetCashFlow.mockResolvedValue({
      currency: 'USD',
      points: [
        {
          from: '2026-09-01',
          to: '2026-09-30',
          inflow: '2000.00',
          outflow: '150.00',
          net: '1850.00',
        },
      ],
    })
    mockedGetTrends.mockResolvedValue({
      currency: 'USD',
      current: {
        from: '2026-09-01',
        to: '2026-09-30',
        inflow: '2000.00',
        outflow: '150.00',
        net: '1850.00',
      },
      previous: {
        from: '2026-08-01',
        to: '2026-08-31',
        inflow: '1800.00',
        outflow: '200.00',
        net: '1600.00',
      },
      inflow_change_pct: 11.1,
      outflow_change_pct: -25,
    })
    mockedGetSavingsRate.mockResolvedValue({
      currency: 'USD',
      income: '2000.00',
      outflow: '150.00',
      net: '1850.00',
      rate: 0.925,
    })

    renderPage()

    // The chart itself renders through recharts' SVG (its text nodes
    // split across <tspan>s, which getByText's default exact-node match
    // doesn't reliably see under jsdom's ResizeObserver stub) — this
    // asserts the screen actually reached the "has data" branch and
    // rendered each metric's own plain-text figures as the API returned
    // them, not on chart internals.
    expect(
      await screen.findByText('Spending & income by category'),
    ).toBeInTheDocument()
    expect(screen.getByText('Cash flow')).toBeInTheDocument()
    expect(screen.getByText('1850.00 USD')).toBeInTheDocument()
    expect(screen.getByText('92.5%')).toBeInTheDocument()
    expect(screen.getByText(/Inflow \+11\.1%/)).toBeInTheDocument()
  })

  // Additional stats (issue #196): top transactions, average transaction
  // size, and per-category trend deltas.
  it('renders top transactions, average size, and category trends from the API as-is', async () => {
    mockedGetCategoryBreakdown.mockResolvedValue({ currency: 'USD', rows: [] })
    mockedGetCashFlow.mockResolvedValue({ currency: 'USD', points: [] })
    mockedGetTrends.mockResolvedValue(emptyTrends)
    mockedGetSavingsRate.mockResolvedValue({
      currency: 'USD',
      income: '0.00',
      outflow: '0.00',
      net: '0.00',
      rate: null,
    })
    mockedGetTopTransactions.mockResolvedValue({
      currency: 'USD',
      rows: [
        {
          transaction_id: 't1',
          description: 'Rent',
          date: '2026-09-01',
          category: 'Housing',
          amount: '-1200.00',
        },
      ],
    })
    mockedGetAverageTransactionSize.mockResolvedValue({
      currency: 'USD',
      overall: { category: 'Uncategorized', count: 4, average: '87.50' },
      by_category: [{ category: 'Food', count: 3, average: '50.00' }],
    })
    mockedGetCategoryTrends.mockResolvedValue({
      currency: 'USD',
      current_from: '2026-09-01',
      current_to: '2026-09-30',
      previous_from: '2026-08-01',
      previous_to: '2026-08-31',
      rows: [
        {
          category: 'Food',
          current: {
            category: 'Food',
            spending: '180.00',
            income: '0.00',
            net: '-180.00',
          },
          previous: {
            category: 'Food',
            spending: '150.00',
            income: '0.00',
            net: '-150.00',
          },
          spending_change_pct: 20,
          income_change_pct: null,
        },
      ],
    })

    renderPage()

    expect(await screen.findByText('Top transactions')).toBeInTheDocument()
    expect(screen.getByText('Rent')).toBeInTheDocument()
    expect(screen.getByText('-1200.00 USD')).toBeInTheDocument()

    expect(screen.getByText('Average transaction size')).toBeInTheDocument()
    expect(screen.getByText('87.50 USD')).toBeInTheDocument()
    expect(screen.getByText('50.00 USD')).toBeInTheDocument()

    expect(screen.getByText('Category trends')).toBeInTheDocument()
    expect(screen.getByText('180.00 USD')).toBeInTheDocument()
    expect(screen.getByText(/\+20\.0%/)).toBeInTheDocument()
  })

  it('shows an empty state when nothing matches the period', async () => {
    mockedGetCategoryBreakdown.mockResolvedValue({ currency: 'USD', rows: [] })
    mockedGetCashFlow.mockResolvedValue({ currency: 'USD', points: [] })
    mockedGetTrends.mockResolvedValue(emptyTrends)
    mockedGetSavingsRate.mockResolvedValue({
      currency: 'USD',
      income: '0.00',
      outflow: '0.00',
      net: '0.00',
      rate: null,
    })

    renderPage()

    expect(await screen.findByText('Nothing to show yet')).toBeInTheDocument()
  })

  it('shows the server error on failure', async () => {
    mockedGetCategoryBreakdown.mockRejectedValue(
      new ApiError('internal', 'Something went wrong. Try again in a moment.'),
    )
    mockedGetCashFlow.mockResolvedValue({ currency: 'USD', points: [] })
    mockedGetTrends.mockResolvedValue(emptyTrends)
    mockedGetSavingsRate.mockResolvedValue({
      currency: 'USD',
      income: '0.00',
      outflow: '0.00',
      net: '0.00',
      rate: null,
    })

    renderPage()

    expect(
      await screen.findByText('Something went wrong. Try again in a moment.'),
    ).toBeInTheDocument()
  })

  it('hides the currency selector for a single-currency ledger', async () => {
    mockedGetCategoryBreakdown.mockResolvedValue({ currency: 'USD', rows: [] })
    mockedGetCashFlow.mockResolvedValue({ currency: 'USD', points: [] })
    mockedGetTrends.mockResolvedValue(emptyTrends)
    mockedGetSavingsRate.mockResolvedValue({
      currency: 'USD',
      income: '0.00',
      outflow: '0.00',
      net: '0.00',
      rate: null,
    })

    renderPage()

    await screen.findByText('Nothing to show yet')
    expect(screen.queryByLabelText('Currency')).not.toBeInTheDocument()
  })

  it('shows the currency selector once a second currency is in use', async () => {
    mockedListAccounts.mockResolvedValue([
      {
        id: 'a1',
        name: 'Checking',
        type: 'bank',
        currency: 'USD',
        archived: false,
        opening_balance: '0.00',
        sort_order: 0,
      },
      {
        id: 'a2',
        name: 'Reisekonto',
        type: 'bank',
        currency: 'EUR',
        archived: false,
        opening_balance: '0.00',
        sort_order: 1,
      },
    ])
    mockedGetCategoryBreakdown.mockResolvedValue({ currency: 'USD', rows: [] })
    mockedGetCashFlow.mockResolvedValue({ currency: 'USD', points: [] })
    mockedGetTrends.mockResolvedValue(emptyTrends)
    mockedGetSavingsRate.mockResolvedValue({
      currency: 'USD',
      income: '0.00',
      outflow: '0.00',
      net: '0.00',
      rate: null,
    })

    renderPage()

    expect(await screen.findByLabelText('Currency')).toBeInTheDocument()
  })

  it('surfaces unconverted postings with a refresh-rates affordance', async () => {
    mockedGetCategoryBreakdown.mockResolvedValue({
      currency: 'USD',
      rows: [
        { category: 'Food', spending: '10.00', income: '0.00', net: '-10.00' },
      ],
      unconverted: [
        {
          transaction_id: 't1',
          amount: '500',
          currency: 'JPY',
          reason: 'no rate on file',
        },
      ],
    })
    mockedGetCashFlow.mockResolvedValue({ currency: 'USD', points: [] })
    mockedGetTrends.mockResolvedValue(emptyTrends)
    mockedGetSavingsRate.mockResolvedValue({
      currency: 'USD',
      income: '0.00',
      outflow: '0.00',
      net: '0.00',
      rate: null,
    })

    renderPage()

    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /Refresh rates/i }),
      ).toBeInTheDocument(),
    )
    expect(
      screen.getByText(/Not converted — no rate on file/),
    ).toBeInTheDocument()
  })

  // Granularity selector (issue #194).
  describe('granularity selector', () => {
    function mockEmptyResults() {
      mockedGetCategoryBreakdown.mockResolvedValue({
        currency: 'USD',
        rows: [],
      })
      mockedGetCashFlow.mockResolvedValue({ currency: 'USD', points: [] })
      mockedGetTrends.mockResolvedValue(emptyTrends)
      mockedGetSavingsRate.mockResolvedValue({
        currency: 'USD',
        income: '0.00',
        outflow: '0.00',
        net: '0.00',
        rate: null,
      })
    }

    it('defaults to month, unchanged from before the selector existed', async () => {
      mockEmptyResults()
      renderPage()

      await screen.findByText('Nothing to show yet')
      expect(mockedGetCashFlow).toHaveBeenCalledWith(
        expect.anything(),
        expect.anything(),
        'month',
      )
      expect(mockedGetTrends).toHaveBeenCalledWith(
        expect.anything(),
        'month',
        expect.anything(),
      )
      expect(
        screen.getByRole('combobox', { name: 'Granularity' }),
      ).toBeInTheDocument()
    })

    it('refetches cash flow and trends with the chosen granularity', async () => {
      mockEmptyResults()
      renderPage()
      await screen.findByText('Nothing to show yet')

      const user = userEvent.setup()
      await user.click(screen.getByRole('combobox', { name: 'Granularity' }))
      await user.click(await screen.findByRole('option', { name: 'Week' }))

      await waitFor(() =>
        expect(mockedGetCashFlow).toHaveBeenLastCalledWith(
          expect.anything(),
          expect.anything(),
          'week',
        ),
      )
      expect(mockedGetTrends).toHaveBeenLastCalledWith(
        expect.anything(),
        'week',
        expect.anything(),
      )
    })

    it('holds off fetching a custom range until both dates are set', async () => {
      mockEmptyResults()
      renderPage()
      await screen.findByText('Nothing to show yet')
      const callsBefore = mockedGetCashFlow.mock.calls.length

      const user = userEvent.setup()
      await user.click(screen.getByRole('combobox', { name: 'Granularity' }))
      await user.click(await screen.findByRole('option', { name: 'Custom' }))

      expect(
        await screen.findByText(
          'Select both From and To to use a custom range.',
        ),
      ).toBeInTheDocument()
      expect(mockedGetCashFlow.mock.calls.length).toBe(callsBefore)
    })
  })
})
