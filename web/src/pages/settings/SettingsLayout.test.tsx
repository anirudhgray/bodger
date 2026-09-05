// Component tests for the Settings sub-nav itself (issue #107): proves
// the four tabs are real links to their own routes, that the tab
// matching the current URL is marked active, and that clicking another
// tab actually navigates there (mounted through a router, not just
// asserting on SettingsLayout in isolation, since the whole point is
// real cross-page navigation rather than in-place content switching).
import { render, screen } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { SettingsLayout } from './SettingsLayout'

function renderAt(path: string) {
  const router = createMemoryRouter(
    [
      {
        path: '/settings',
        element: <SettingsLayout />,
        children: [
          { path: 'password', element: <div>Password page</div> },
          { path: 'tokens', element: <div>Tokens page</div> },
          { path: 'accounts', element: <div>Accounts page</div> },
          { path: 'categories', element: <div>Categories page</div> },
        ],
      },
    ],
    { initialEntries: [path] },
  )
  render(<RouterProvider router={router} />)
  return router
}

describe('SettingsLayout', () => {
  it('renders a tab per section, each a real link to its own route', () => {
    renderAt('/settings/password')

    for (const [name, href] of [
      ['Password', '/settings/password'],
      ['API tokens', '/settings/tokens'],
      ['Accounts', '/settings/accounts'],
      ['Categories', '/settings/categories'],
    ]) {
      const link = screen.getByRole('tab', { name })
      expect(link.tagName).toBe('A')
      expect(link).toHaveAttribute('href', href)
    }
  })

  it('marks the tab matching the current URL as selected', async () => {
    renderAt('/settings/accounts')

    expect(await screen.findByText('Accounts page')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Accounts' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getByRole('tab', { name: 'Password' })).toHaveAttribute(
      'aria-selected',
      'false',
    )
  })

  it('navigates to another subpage when its tab is clicked', async () => {
    renderAt('/settings/password')
    expect(await screen.findByText('Password page')).toBeInTheDocument()

    screen.getByRole('tab', { name: 'Categories' }).click()

    expect(await screen.findByText('Categories page')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Categories' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
  })
})
