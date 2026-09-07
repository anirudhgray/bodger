// Component tests for the Password settings subpage (issue #107, split
// out of the old Settings.test.tsx). web/src/lib/settings is mocked here
// so these exercise only the page's own logic against a controlled fake
// API, the same pattern Login.test.tsx uses for web/src/lib/session.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/settings', () => ({
  changePassword: vi.fn(),
}))

// Changing the password is a user-triggered action, so its failure now
// reports via a toast rather than inline (docs/design-system.md's
// "Toasts vs. inline messages") — mocked here so tests can assert on
// what was shown without a real <Toaster/> mounted.
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    promise: vi.fn(),
  },
}))

import { ApiError } from '@/lib/api'
import { changePassword } from '@/lib/settings'
import { PasswordSettings } from './Password'
import { toast } from 'sonner'

const mockedChangePassword = vi.mocked(changePassword)
const mockedToastError = vi.mocked(toast.error)

function renderPage() {
  render(
    <MemoryRouter initialEntries={['/settings/password']}>
      <Routes>
        <Route path="/settings/password" element={<PasswordSettings />} />
        <Route path="/login" element={<div>Login screen</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('PasswordSettings', () => {
  beforeEach(() => {
    mockedChangePassword.mockReset()
    mockedToastError.mockReset()
  })

  it('changes the password and redirects to login, since the session is revoked', async () => {
    mockedChangePassword.mockResolvedValue(undefined)
    renderPage()

    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'new-password-123' },
    })
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'new-password-123' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }))

    expect(await screen.findByText('Login screen')).toBeInTheDocument()
    expect(mockedChangePassword).toHaveBeenCalledWith('new-password-123')
  })

  it('rejects a password change when the confirmation does not match', async () => {
    renderPage()

    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'new-password-123' },
    })
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'something-else' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }))

    // Changing the password is a user-triggered action (docs/design-
    // system.md's "Toasts vs. inline messages"), so even this
    // client-side mismatch check reports via a toast like any other
    // action — but paired with a persistent inline alert too, one of the
    // two deliberate exceptions (with Login) that keep both, since this
    // is an auth-credential screen.
    await waitFor(() =>
      expect(mockedToastError).toHaveBeenCalledWith('Passwords didn’t match.'),
    )
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Passwords didn’t match.',
    )
    expect(mockedChangePassword).not.toHaveBeenCalled()
  })

  it('shows both a toast and a persistent inline alert on a failed password change', async () => {
    mockedChangePassword.mockRejectedValue(
      new ApiError(
        'invalid_input',
        'A password must be at least 8 characters.',
      ),
    )
    renderPage()

    fireEvent.change(screen.getByLabelText('New password'), {
      target: { value: 'short' },
    })
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'short' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Change password' }))

    await waitFor(() =>
      expect(mockedToastError).toHaveBeenCalledWith(
        'A password must be at least 8 characters.',
      ),
    )
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'A password must be at least 8 characters.',
    )
  })
})
