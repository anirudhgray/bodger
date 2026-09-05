// Component tests for the balances screen (issue #63). lib/api's
// getBalances is mocked so these exercise only the screen's own
// rendering: loading, the real per-account list, an empty ledger, and a
// server error — not apiFetch's own behaviour (covered in api.test.ts).
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return { ...actual, getBalances: vi.fn() }
})

import { ApiError, getBalances } from '@/lib/api'
import { BalancesPage } from './Balances'

const mockedGetBalances = vi.mocked(getBalances)

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
})
