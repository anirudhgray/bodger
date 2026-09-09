// Component tests for the balances screen (issue #63, extended by #139
// for currency conversion). lib/api's getBalances/getReportingCurrency/
// fetchFxRates are mocked so these exercise only the screen's own
// rendering: loading, the real per-account list, an empty ledger, a
// server error, and (issue #139) the currency selector, rate-provenance
// detail row, unconverted flagging, and refresh popover — not apiFetch's
// own behaviour (covered in api.test.ts).
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    getBalances: vi.fn(),
    getReportingCurrency: vi.fn(),
    fetchFxRates: vi.fn(),
  }
})

import {
  ApiError,
  fetchFxRates,
  getBalances,
  getReportingCurrency,
} from '@/lib/api'
import { BalancesPage } from './Balances'

const mockedGetBalances = vi.mocked(getBalances)
const mockedGetReportingCurrency = vi.mocked(getReportingCurrency)
const mockedFetchFxRates = vi.mocked(fetchFxRates)

// Renders the account filter the router state carried to /transactions,
// standing in for the real TransactionsList so this test only asserts on
// navigation, not on that screen's own rendering (covered in
// TransactionsList.test.tsx).
function TransactionsProbe() {
  const location = useLocation()
  const filter = (location.state as { filter?: { account?: string } } | null)
    ?.filter
  return <div>account filter: {filter?.account ?? 'none'}</div>
}

// The empty state links to /settings (Empty's "Add an account" CTA), which
// needs a router context even though most of this suite never reaches it.
function renderPage() {
  return render(
    <MemoryRouter>
      <BalancesPage />
    </MemoryRouter>,
  )
}

describe('BalancesPage', () => {
  beforeEach(() => {
    mockedGetBalances.mockReset()
    mockedGetReportingCurrency.mockReset()
    mockedGetReportingCurrency.mockResolvedValue({
      currency: '',
      is_set: false,
      effectiveCurrency: 'USD',
    })
    mockedFetchFxRates.mockReset()
  })

  it('renders every account balance exactly as the API returned it', async () => {
    mockedGetBalances.mockResolvedValue({
      as_of: '2026-09-03',
      balances: [
        {
          account_id: 'a1',
          account: 'Checking',
          amount: '1500.00',
          currency: 'USD',
        },
        {
          account_id: 'a2',
          account: 'Savings',
          amount: '250000.00',
          currency: 'JPY',
        },
      ],
    })

    renderPage()

    expect(await screen.findByText('Checking')).toBeInTheDocument()
    expect(screen.getByText('1500.00 USD')).toBeInTheDocument()
    expect(screen.getByText('Savings')).toBeInTheDocument()
    expect(screen.getByText('250000.00 JPY')).toBeInTheDocument()
    expect(screen.getByText('As of 2026-09-03')).toBeInTheDocument()
  })

  it('shows an empty state for a ledger with no accounts', async () => {
    mockedGetBalances.mockResolvedValue({ as_of: '2026-09-03', balances: [] })

    renderPage()

    expect(await screen.findByText('No accounts yet')).toBeInTheDocument()
  })

  it('shows the server error on failure', async () => {
    mockedGetBalances.mockRejectedValue(
      new ApiError('internal', 'Something went wrong. Try again in a moment.'),
    )

    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Something went wrong. Try again in a moment.',
    )
  })

  it('navigates to transactions filtered by account when a row is clicked', async () => {
    mockedGetBalances.mockResolvedValue({
      as_of: '2026-09-03',
      balances: [
        {
          account_id: 'a1',
          account: 'Checking',
          amount: '1500.00',
          currency: 'USD',
        },
      ],
    })

    render(
      <MemoryRouter initialEntries={['/balances']}>
        <Routes>
          <Route path="/balances" element={<BalancesPage />} />
          <Route path="/transactions" element={<TransactionsProbe />} />
        </Routes>
      </MemoryRouter>,
    )

    fireEvent.click(await screen.findByText('Checking'))

    expect(await screen.findByText('account filter: a1')).toBeInTheDocument()
  })

  it('shows no currency selector for a single-currency ledger', async () => {
    mockedGetBalances.mockResolvedValue({
      as_of: '2026-09-03',
      balances: [
        {
          account_id: 'a1',
          account: 'Checking',
          amount: '1500.00',
          currency: 'USD',
        },
        {
          account_id: 'a2',
          account: 'Savings',
          amount: '2000.00',
          currency: 'USD',
        },
      ],
    })

    renderPage()

    await screen.findByText('Checking')
    expect(
      screen.queryByRole('combobox', { name: 'Show in' }),
    ).not.toBeInTheDocument()
  })

  it('converts balances into the chosen currency and shows rate provenance on demand', async () => {
    const user = userEvent.setup()
    mockedGetReportingCurrency.mockResolvedValue({
      currency: 'EUR',
      is_set: true,
      effectiveCurrency: 'EUR',
    })
    mockedGetBalances.mockResolvedValueOnce({
      as_of: '2026-09-03',
      balances: [
        {
          account_id: 'a1',
          account: 'Checking',
          amount: '1500.00',
          currency: 'USD',
        },
        {
          account_id: 'a2',
          account: 'Savings',
          amount: '900.00',
          currency: 'EUR',
        },
      ],
    })
    mockedGetBalances.mockResolvedValueOnce({
      as_of: '2026-09-03',
      balances: [
        {
          account_id: 'a1',
          account: 'Checking',
          amount: '1500.00',
          currency: 'USD',
          converted: {
            amount: '1380.00',
            currency: 'EUR',
            policy: 'current',
            rate: '0.92',
            rate_date: '2026-09-03',
            rate_source: 'frankfurter',
            stale: true,
          },
        },
        {
          account_id: 'a2',
          account: 'Savings',
          amount: '900.00',
          currency: 'EUR',
        },
      ],
    })

    renderPage()
    await screen.findByText('Checking')

    await user.click(screen.getByRole('combobox', { name: 'Show in' }))
    await user.click(
      await screen.findByRole('option', { name: /EUR \(reporting currency\)/ }),
    )

    expect(mockedGetBalances).toHaveBeenLastCalledWith({
      currency: 'EUR',
      policy: 'current',
    })

    const approx = await screen.findByText('≈ 1380.00 EUR')
    expect(screen.getByText('stale')).toBeInTheDocument()

    await user.click(approx)
    expect(
      screen.getByText(/1 USD = 0.92 EUR.*frankfurter/),
    ).toBeInTheDocument()
  })

  it('flags an unconverted account with its reason instead of dropping it', async () => {
    const user = userEvent.setup()
    mockedGetBalances.mockResolvedValueOnce({
      as_of: '2026-09-03',
      balances: [
        {
          account_id: 'a1',
          account: 'Checking',
          amount: '1500.00',
          currency: 'USD',
        },
        {
          account_id: 'a2',
          account: 'Rupee wallet',
          amount: '5000.00',
          currency: 'INR',
        },
      ],
    })
    mockedGetBalances.mockResolvedValueOnce({
      as_of: '2026-09-03',
      balances: [
        {
          account_id: 'a1',
          account: 'Checking',
          amount: '1500.00',
          currency: 'USD',
        },
        {
          account_id: 'a2',
          account: 'Rupee wallet',
          amount: '5000.00',
          currency: 'INR',
        },
      ],
      unconverted: [
        { account: 'Rupee wallet', reason: 'no USD/INR rate available' },
      ],
    })

    renderPage()
    await screen.findByText('Checking')

    await user.click(screen.getByRole('combobox', { name: 'Show in' }))
    await user.click(await screen.findByRole('option', { name: 'USD' }))

    expect(
      await screen.findByText('Not converted — no USD/INR rate available'),
    ).toBeInTheDocument()
  })

  it('refreshes only the picked pairs and reloads balances', async () => {
    const user = userEvent.setup()
    mockedGetBalances.mockResolvedValueOnce({
      as_of: '2026-09-03',
      balances: [
        {
          account_id: 'a1',
          account: 'Checking',
          amount: '1500.00',
          currency: 'USD',
        },
        {
          account_id: 'a2',
          account: 'Savings',
          amount: '900.00',
          currency: 'EUR',
        },
      ],
    })
    mockedGetBalances.mockResolvedValueOnce({
      as_of: '2026-09-03',
      balances: [
        {
          account_id: 'a1',
          account: 'Checking',
          amount: '1500.00',
          currency: 'USD',
        },
        {
          account_id: 'a2',
          account: 'Savings',
          amount: '900.00',
          currency: 'EUR',
        },
      ],
    })
    mockedFetchFxRates.mockResolvedValue({
      reporting_currency: 'EUR',
      fetched: [
        {
          pair: 'USD/EUR',
          rate: '0.93',
          date: '2026-09-03',
          source: 'frankfurter',
        },
      ],
    })
    mockedGetBalances.mockResolvedValueOnce({
      as_of: '2026-09-03',
      balances: [
        {
          account_id: 'a1',
          account: 'Checking',
          amount: '1500.00',
          currency: 'USD',
          converted: {
            amount: '1400.00',
            currency: 'EUR',
            policy: 'current',
            rate: '0.93',
            rate_date: '2026-09-03',
            rate_source: 'frankfurter',
            stale: false,
          },
        },
        {
          account_id: 'a2',
          account: 'Savings',
          amount: '900.00',
          currency: 'EUR',
        },
      ],
    })

    renderPage()
    await screen.findByText('Checking')

    await user.click(screen.getByRole('combobox', { name: 'Show in' }))
    await user.click(await screen.findByRole('option', { name: 'EUR' }))
    await screen.findByText('900.00 EUR')

    await user.click(screen.getByRole('button', { name: /Refresh rates/ }))
    const checkbox = await screen.findByRole('checkbox', { name: 'USD' })
    await user.click(checkbox)
    await user.click(screen.getByRole('button', { name: 'Refresh' }))

    expect(mockedFetchFxRates).toHaveBeenCalledWith(['USD'], undefined, 'EUR')
    expect(await screen.findByText('≈ 1400.00 EUR')).toBeInTheDocument()
  })
})
