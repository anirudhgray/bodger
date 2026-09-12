// Component test for AppLayout's logout button (issue #59). AppLayout's
// own loader (routes.tsx's requireAuth) already guarantees an
// authenticated actor by the time it renders — see routes.test.tsx for
// that guard — so this test mounts AppLayout directly behind a plain
// (non-data) router, exercising only its own logout click handler.
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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
    const user = userEvent.setup()
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
    // The Category field is a Combobox (issue #106), not a native
    // <select> — see TransactionDialog.test.tsx's pickCategory for the
    // same real-interaction convention.
    await user.click(await screen.findByRole('combobox', { name: 'Category' }))
    await user.click(await screen.findByRole('option', { name: 'Coffee' }))
    fireEvent.click(screen.getByRole('button', { name: 'Record spend' }))

    expect(await screen.findByText('Transactions content')).toBeInTheDocument()
  })

  describe('Settings sidebar submenu', () => {
    // The sidebar's Settings entry (App.tsx's AppSidebar) is a second,
    // collapsed way to reach the same four routes SettingsLayout's tab
    // strip already covers (see AppSidebar's comment for why both exist).
    // These assert the collapse/expand behavior itself, not the routes'
    // own content — SettingsLayout.test.tsx already covers the tab strip.
    function renderAt(path: string) {
      render(
        <ThemeProvider>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route path="/" element={<AppLayout />}>
                <Route
                  path="transactions"
                  element={<div>Transactions content</div>}
                />
                <Route
                  path="settings/accounts"
                  element={<div>Accounts content</div>}
                />
                <Route
                  path="settings/categories"
                  element={<div>Categories content</div>}
                />
              </Route>
            </Routes>
          </MemoryRouter>
        </ThemeProvider>,
      )
    }

    it('starts collapsed on a non-settings route', () => {
      renderAt('/transactions')

      expect(
        screen.getByRole('button', { name: /Settings/ }),
      ).toBeInTheDocument()
      expect(
        screen.queryByRole('link', { name: 'Accounts' }),
      ).not.toBeInTheDocument()
    })

    it('expands to show every section on click, and collapses again', () => {
      renderAt('/transactions')

      const trigger = screen.getByRole('button', { name: /Settings/ })
      fireEvent.click(trigger)

      for (const name of ['Password', 'API tokens', 'Accounts', 'Categories']) {
        expect(screen.getByRole('link', { name })).toHaveAttribute(
          'href',
          `/settings/${name === 'API tokens' ? 'tokens' : name.toLowerCase()}`,
        )
      }

      fireEvent.click(trigger)
      expect(
        screen.queryByRole('link', { name: 'Accounts' }),
      ).not.toBeInTheDocument()
    })

    it('starts expanded when already on a settings route, with that section current', () => {
      renderAt('/settings/accounts')

      const accountsLink = screen.getByRole('link', { name: 'Accounts' })
      expect(accountsLink).toHaveAttribute('aria-current', 'page')
      expect(
        screen.getByRole('link', { name: 'Categories' }),
      ).not.toHaveAttribute('aria-current')
    })

    it('navigates when a section link is clicked', async () => {
      renderAt('/settings/accounts')

      fireEvent.click(screen.getByRole('link', { name: 'Categories' }))

      expect(await screen.findByText('Categories content')).toBeInTheDocument()
    })
  })

  describe('Import & export sidebar submenu', () => {
    // Issue #214's own nav entry — the same collapsible-group shape as
    // Settings above, and the only way to reach import/export/restore
    // (there's no in-page tab strip duplicating this nav).
    function renderAt(path: string) {
      render(
        <ThemeProvider>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route path="/" element={<AppLayout />}>
                <Route
                  path="transactions"
                  element={<div>Transactions content</div>}
                />
                <Route
                  path="import-export/import"
                  element={<div>Import content</div>}
                />
                <Route
                  path="import-export/export"
                  element={<div>Export content</div>}
                />
              </Route>
            </Routes>
          </MemoryRouter>
        </ThemeProvider>,
      )
    }

    it('starts collapsed on a non-import-export route', () => {
      renderAt('/transactions')

      expect(
        screen.getByRole('button', { name: /Import & export/ }),
      ).toBeInTheDocument()
      expect(
        screen.queryByRole('link', { name: 'Import' }),
      ).not.toBeInTheDocument()
    })

    it('expands to show every section on click, and collapses again', () => {
      renderAt('/transactions')

      const trigger = screen.getByRole('button', { name: /Import & export/ })
      fireEvent.click(trigger)

      for (const [name, slug] of [
        ['Import', 'import'],
        ['Export', 'export'],
        ['Restore', 'restore'],
      ]) {
        expect(screen.getByRole('link', { name })).toHaveAttribute(
          'href',
          `/import-export/${slug}`,
        )
      }

      fireEvent.click(trigger)
      expect(
        screen.queryByRole('link', { name: 'Import' }),
      ).not.toBeInTheDocument()
    })

    it('starts expanded when already on an import-export route, with that section current', () => {
      renderAt('/import-export/export')

      const exportLink = screen.getByRole('link', { name: 'Export' })
      expect(exportLink).toHaveAttribute('aria-current', 'page')
      expect(screen.getByRole('link', { name: 'Import' })).not.toHaveAttribute(
        'aria-current',
      )
    })

    it('navigates when a section link is clicked', async () => {
      renderAt('/import-export/export')

      fireEvent.click(screen.getByRole('link', { name: 'Import' }))

      expect(await screen.findByText('Import content')).toBeInTheDocument()
    })
  })
})
