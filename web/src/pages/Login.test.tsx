// Component tests for the login form itself (issue #59) — separate from
// routes.test.tsx, which covers the loader-driven redirect behaviour
// around it. lib/session's login is mocked here so these tests exercise
// only the form's own logic: submit disabled on an empty password,
// success navigates away, failure shows the server's message and leaves
// the form usable again.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/session', () => ({
  login: vi.fn(),
}))

// Logging in is a user-triggered action, so its failure now reports via
// a toast rather than inline (docs/design-system.md's "Toasts vs.
// inline messages") — mocked here so tests can assert on what was shown
// without a real <Toaster/> mounted.
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    promise: vi.fn(),
  },
}))

import { ApiError } from '@/lib/api'
import { login } from '@/lib/session'
import { Login } from './Login'
import { toast } from 'sonner'

const mockedLogin = vi.mocked(login)
const mockedToastError = vi.mocked(toast.error)

function renderLogin() {
  render(
    <MemoryRouter initialEntries={['/login']}>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/" element={<div>Protected home</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('Login', () => {
  beforeEach(() => {
    mockedLogin.mockReset()
    mockedToastError.mockReset()
  })

  it('disables submit until a password is entered', () => {
    renderLogin()

    expect(screen.getByRole('button', { name: 'Log in' })).toBeDisabled()

    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'hunter2' },
    })

    expect(screen.getByRole('button', { name: 'Log in' })).toBeEnabled()
  })

  it('navigates to the protected route on a successful login', async () => {
    mockedLogin.mockResolvedValue(undefined)
    renderLogin()

    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'hunter2' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Log in' }))

    expect(await screen.findByText('Protected home')).toBeInTheDocument()
    expect(mockedLogin).toHaveBeenCalledWith('hunter2')
  })

  it('shows a toast (not inline) and leaves the form usable on failure', async () => {
    mockedLogin.mockRejectedValue(
      new ApiError('unauthenticated', 'Incorrect password.'),
    )
    renderLogin()

    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'wrong' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Log in' }))

    // Logging in is a user-triggered action (docs/design-system.md's
    // "Toasts vs. inline messages"), so its failure is reported via a
    // toast, not inline.
    await waitFor(() =>
      expect(mockedToastError).toHaveBeenCalledWith('Incorrect password.'),
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Log in' })).toBeEnabled()
  })
})
