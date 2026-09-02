// Smoke test for the routing skeleton (issue #58): proves the route tree
// actually mounts and each top-level path resolves to its placeholder, so
// a broken import or route definition fails `make test` rather than only
// showing up when someone next opens the app. Not a place for anything
// resembling behaviour these placeholders don't have yet — the real
// screens (#59-#63) bring their own component tests.
//
// Each case builds its own createMemoryRouter from the same routes tree
// routes.tsx feeds the real createBrowserRouter, rather than sharing one
// router instance across cases — a memory router's starting location is
// fixed at creation, so this is the straightforward way to test more than
// one URL in the same file.
import { render, screen } from '@testing-library/react'
import {
  createMemoryRouter,
  RouterProvider,
  type RouteObject,
} from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { routes, RouteError } from './routes'

describe('routes', () => {
  it('redirects the root route to the transactions placeholder', async () => {
    const router = createMemoryRouter(routes, { initialEntries: ['/'] })
    render(<RouterProvider router={router} />)

    expect(
      await screen.findByRole('heading', { name: 'Transactions' }),
    ).toBeInTheDocument()
  })

  it('renders the login placeholder at /login', async () => {
    const router = createMemoryRouter(routes, { initialEntries: ['/login'] })
    render(<RouterProvider router={router} />)

    expect(
      await screen.findByRole('heading', { name: 'Log in' }),
    ).toBeInTheDocument()
  })

  it.each([
    ['/transactions/new', 'Add a transaction'],
    ['/balances', 'Balances'],
    ['/settings', 'Settings'],
  ])('renders the placeholder at %s', async (path, heading) => {
    const router = createMemoryRouter(routes, { initialEntries: [path] })
    render(<RouterProvider router={router} />)

    expect(
      await screen.findByRole('heading', { name: heading }),
    ).toBeInTheDocument()
  })

  // Regression coverage for a real bug: /api/v1 (not a registered REST
  // route, so the server's SPA fallback correctly serves index.html) used
  // to hit react-router-dom's own default crash screen client-side, since
  // nothing in the route tree matched it and there was no errorElement.
  it('renders a not-found placeholder for a path nothing matches', async () => {
    const router = createMemoryRouter(routes, {
      initialEntries: ['/api/v1'],
    })
    render(<RouterProvider router={router} />)

    expect(
      await screen.findByRole('heading', { name: 'Page not found' }),
    ).toBeInTheDocument()
  })

  it('renders RouteError instead of the default crash screen when a route throws', async () => {
    function Throws(): never {
      throw new Error('boom')
    }
    const throwingRoutes: RouteObject[] = [
      { path: '/', element: <Throws />, errorElement: <RouteError /> },
    ]
    const router = createMemoryRouter(throwingRoutes, {
      initialEntries: ['/'],
    })
    render(<RouterProvider router={router} />)

    expect(
      await screen.findByRole('heading', { name: 'Something went wrong' }),
    ).toBeInTheDocument()
  })
})
