// Component tests for the analytics screen (issue #189). lib/api's four
// analytics getters plus getReportingCurrency/listAccounts/fetchFxRates
// are mocked so these exercise only the screen's own rendering: loading,
// real chart data, an empty period, a server error, and the unconverted-
// postings banner with its refresh popover.
import { render, screen, waitFor } from '@testing-library/react'
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
    getReportingCurrency: vi.fn(),
    listAccounts: vi.fn(),
    fetchFxRates: vi.fn(),
  }
})

import {
  ApiError,
  getCashFlow,
  getCategoryBreakdown,
  getReportingCurrency,
  getSavingsRate,
  getTrends,
  listAccounts,
} from '@/lib/api'
import { AnalyticsPage } from './Analytics'

const mockedGetCategoryBreakdown = vi.mocked(getCategoryBreakdown)
const mockedGetCashFlow = vi.mocked(getCashFlow)
const mockedGetTrends = vi.mocked(getTrends)
const mockedGetSavingsRate = vi.mocked(getSavingsRate)
const mockedGetReportingCurrency = vi.mocked(getReportingCurrency)
const mockedListAccounts = vi.mocked(listAccounts)

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
    mockedGetReportingCurrency.mockReset()
    mockedListAccounts.mockReset()
    mockedGetReportingCurrency.mockResolvedValue({
      currency: '',
      is_set: false,
      effectiveCurrency: 'USD',
    })
    mockedListAccounts.mockResolvedValue([])
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
          month: '2026-09',
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
})
