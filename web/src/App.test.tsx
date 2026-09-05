// Component test for AppLayout's logout button (issue #59). AppLayout's
// own loader (routes.tsx's requireAuth) already guarantees an
// authenticated actor by the time it renders — see routes.test.tsx for
// that guard — so this test mounts AppLayout directly behind a plain
// (non-data) router, exercising only its own logout click handler.
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/session', () => ({
  logout: vi.fn(),
}))

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    listAccounts: vi.fn().mockResolvedValue([]),
    listCategories: vi.fn().mockResolvedValue([]),
    recordOutflow: vi.fn(),
  }
})

import { logout } from '@/lib/session'
import {
  listAccounts,
  listCategories,
  recordOutflow,
  type Transaction,
} from '@/lib/api'
import { AppLayout } from './App'
import { ThemeProvider } from './hooks/use-theme'

const mockedLogout = vi.mocked(logout)
const mockedRecordOutflow = vi.mocked(recordOutflow)
const mockedListAccounts = vi.mocked(listAccounts)
const mockedListCategories = vi.mocked(listCategories)

describe('AppLayout', () => {
  beforeEach(() => {
    mockedLogout.mockReset()
    mockedRecordOutflow.mockReset()
    mockedListAccounts.mockReset().mockResolvedValue([])
    mockedListCategories.mockReset().mockResolvedValue([])
    localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  it('logs out and navigates to /login on click', async () => {
    mockedLogout.mockResolvedValue(undefined)

    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/transactions']}>
          <Routes>
            <Route path="/" element={<AppLayout />}>
              <Route
                path="transactions"
                element={<div>Transactions content</div>}
              />
            </Route>
            <Route path="/login" element={<div>Login page</div>} />
          </Routes>
        </MemoryRouter>
      </ThemeProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Log out' }))

    expect(await screen.findByText('Login page')).toBeInTheDocument()
    expect(mockedLogout).toHaveBeenCalled()
  })

  it('still navigates to /login if the logout request fails', async () => {
    // mockImplementation, not mockRejectedValue: the latter constructs its
    // rejected promise once, up front, at configuration time — with
    // render() and fireEvent.click() between that line and the actual
    // `await logout()` inside handleLogout, Node's unhandled-rejection
    // detector can fire before anything gets a chance to catch it, even
    // though this test does eventually await it. A lazy implementation
    // only creates the promise (and immediately hands it to handleLogout's
    // await) at the moment it's actually called.
    mockedLogout.mockImplementation(() =>
      Promise.reject(new Error('network error')),
    )

    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/transactions']}>
          <Routes>
            <Route path="/" element={<AppLayout />}>
              <Route
                path="transactions"
                element={<div>Transactions content</div>}
              />
            </Route>
            <Route path="/login" element={<div>Login page</div>} />
          </Routes>
        </MemoryRouter>
      </ThemeProvider>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Log out' }))

    expect(await screen.findByText('Login page')).toBeInTheDocument()
  })

  it('toggles dark mode and persists the choice', () => {
    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/transactions']}>
          <Routes>
            <Route path="/" element={<AppLayout />}>
              <Route
                path="transactions"
                element={<div>Transactions content</div>}
              />
            </Route>
          </Routes>
        </MemoryRouter>
      </ThemeProvider>,
    )

    const toggle = screen.getByRole('button', { name: 'Switch to dark mode' })
    expect(document.documentElement.classList.contains('dark')).toBe(false)

    fireEvent.click(toggle)

    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('bodger:theme')).toBe('dark')
    expect(
      screen.getByRole('button', { name: 'Switch to light mode' }),
    ).toBeInTheDocument()

    fireEvent.click(
      screen.getByRole('button', { name: 'Switch to light mode' }),
    )

    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(localStorage.getItem('bodger:theme')).toBe('light')
  })

  it('opens the transaction dialog from Add, and navigates to /transactions after recording', async () => {
    const recorded: Transaction = {
      id: 't1',
      type: 'outflow',
      date: '2026-09-05',
      description: 'Coffee',
      amount: '5.00',
      currency: 'USD',
    }
    mockedRecordOutflow.mockResolvedValue(recorded)
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
    ])
    mockedListCategories.mockResolvedValue([
      {
        id: 'c1',
        name: 'Coffee',
        type: 'expense',
        sort_order: 0,
        archived: false,
      },
    ])

    render(
      <ThemeProvider>
        <MemoryRouter initialEntries={['/balances']}>
          <Routes>
            <Route path="/" element={<AppLayout />}>
              <Route path="balances" element={<div>Balances content</div>} />
              <Route
                path="transactions"
                element={<div>Transactions content</div>}
              />
            </Route>
          </Routes>
        </MemoryRouter>
      </ThemeProvider>,
    )

    fireEvent.click(screen.getByText('Add'))
    expect(
      await screen.findByRole('dialog', { name: 'Add transaction' }),
    ).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Amount'), {
      target: { value: '5.00' },
    })
    fireEvent.change(await screen.findByLabelText('Category'), {
      target: { value: 'c1' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Record spend' }))

    expect(await screen.findByText('Transactions content')).toBeInTheDocument()
  })
})
