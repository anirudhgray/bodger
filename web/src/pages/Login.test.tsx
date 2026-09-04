// Component tests for the login form itself (issue #59) — separate from
// routes.test.tsx, which covers the loader-driven redirect behaviour
// around it. lib/session's login is mocked here so these tests exercise
// only the form's own logic: submit disabled on an empty password,
// success navigates away, failure shows the server's message and leaves
// the form usable again.
import { fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/session', () => ({
  login: vi.fn(),
}))

import { ApiError } from '@/lib/api'
import { login } from '@/lib/session'
import { Login } from './Login'

const mockedLogin = vi.mocked(login)

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

  it('shows the server error and leaves the form usable on failure', async () => {
    mockedLogin.mockRejectedValue(
      new ApiError('unauthenticated', 'Incorrect password.'),
    )
    renderLogin()

    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'wrong' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Log in' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Incorrect password.',
    )
    expect(screen.getByRole('button', { name: 'Log in' })).toBeEnabled()
  })
})
