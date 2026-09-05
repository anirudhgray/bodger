// Component tests for the Password settings subpage (issue #107, split
// out of the old Settings.test.tsx). web/src/lib/settings is mocked here
// so these exercise only the page's own logic against a controlled fake
// API, the same pattern Login.test.tsx uses for web/src/lib/session.
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/settings', () => ({
  changePassword: vi.fn(),
}))

import { ApiError } from '@/lib/api'
import { changePassword } from '@/lib/settings'
import { PasswordSettings } from './Password'

const mockedChangePassword = vi.mocked(changePassword)

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

    expect(await screen.findByRole('alert')).toHaveTextContent('didn’t match')
    expect(mockedChangePassword).not.toHaveBeenCalled()
  })

  it('shows the server error on a failed password change', async () => {
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

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'A password must be at least 8 characters.',
    )
  })
})
