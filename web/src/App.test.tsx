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

import { logout } from '@/lib/session'
import { AppLayout } from './App'

const mockedLogout = vi.mocked(logout)

describe('AppLayout', () => {
  beforeEach(() => {
    mockedLogout.mockReset()
    localStorage.clear()
    document.documentElement.classList.remove('dark')
  })

  it('logs out and navigates to /login on click', async () => {
    mockedLogout.mockResolvedValue(undefined)

    render(
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
      </MemoryRouter>,
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
      </MemoryRouter>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Log out' }))

    expect(await screen.findByText('Login page')).toBeInTheDocument()
  })

  it('toggles dark mode and persists the choice', () => {
    render(
      <MemoryRouter initialEntries={['/transactions']}>
        <Routes>
          <Route path="/" element={<AppLayout />}>
            <Route
              path="transactions"
              element={<div>Transactions content</div>}
            />
          </Route>
        </Routes>
      </MemoryRouter>,
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
})
