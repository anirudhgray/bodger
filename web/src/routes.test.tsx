// Smoke test for the routing skeleton (issue #58), plus the auth guard
// issue #59 layered onto it: proves the route tree actually mounts, each
// top-level path resolves to its placeholder, and the '/' and '/login'
// loaders send an unauthenticated or already-authenticated visitor to the
// right place. checkSession is mocked throughout — these are routing
// tests, not a test of the session-probe request itself (session.test.ts
// covers that in isolation) — so every case sets its own resolved value
// rather than relying on a real fetch.
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
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/session', () => ({
  checkSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
}))

// TransactionEntry (issue #60) fetches accounts/categories on mount — its
// own test file (TransactionEntry.test.tsx) covers that screen's actual
// behaviour with realistic data; this file only needs the route to resolve
// to it, so an empty resolved list is enough to let it render without
// throwing.
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    listAccounts: vi.fn().mockResolvedValue([]),
    listCategories: vi.fn().mockResolvedValue([]),
  }
})

import { checkSession } from '@/lib/session'
import { ThemeProvider } from './hooks/use-theme'
import { routes, RouteError } from './routes'

const mockedCheckSession = vi.mocked(checkSession)

describe('routes', () => {
  describe('authenticated', () => {
    beforeEach(() => {
      mockedCheckSession.mockReset().mockResolvedValue(true)
    })

    it('redirects the root route to the transactions placeholder', async () => {
      const router = createMemoryRouter(routes, { initialEntries: ['/'] })
      render(
        <ThemeProvider>
          <RouterProvider router={router} />
        </ThemeProvider>,
      )

      expect(
        await screen.findByRole('heading', { name: 'Transactions' }),
      ).toBeInTheDocument()
    })

    it('redirects /login to the app when the session is already valid', async () => {
      const router = createMemoryRouter(routes, {
        initialEntries: ['/login'],
      })
      render(
        <ThemeProvider>
          <RouterProvider router={router} />
        </ThemeProvider>,
      )

      expect(
        await screen.findByRole('heading', { name: 'Transactions' }),
      ).toBeInTheDocument()
    })
  })

  describe('unauthenticated', () => {
    beforeEach(() => {
      mockedCheckSession.mockReset().mockResolvedValue(false)
    })

    it('renders the login form at /login', async () => {
      const router = createMemoryRouter(routes, {
        initialEntries: ['/login'],
      })
      render(
        <ThemeProvider>
          <RouterProvider router={router} />
        </ThemeProvider>,
      )

      expect(
        await screen.findByRole('heading', { name: 'Log in' }),
      ).toBeInTheDocument()
      expect(screen.getByLabelText('Password')).toBeInTheDocument()
    })

    it('redirects a protected route to the login form', async () => {
      const router = createMemoryRouter(routes, { initialEntries: ['/'] })
      render(
        <ThemeProvider>
          <RouterProvider router={router} />
        </ThemeProvider>,
      )

      expect(
        await screen.findByRole('heading', { name: 'Log in' }),
      ).toBeInTheDocument()
    })
  })

  describe('placeholders (authenticated)', () => {
    beforeEach(() => {
      mockedCheckSession.mockReset().mockResolvedValue(true)
    })

    it.each([['/settings', 'Settings']])(
      'renders the placeholder at %s',
      async (path, heading) => {
        const router = createMemoryRouter(routes, { initialEntries: [path] })
        render(
          <ThemeProvider>
            <RouterProvider router={router} />
          </ThemeProvider>,
        )

        expect(
          await screen.findByRole('heading', { name: heading }),
        ).toBeInTheDocument()
      },
    )

    // /transactions/new used to be its own screen (issue #60); it's now
    // TransactionDialog.test.tsx's territory, opened from the nav rather
    // than navigated to — this just proves an old bookmark still lands
    // somewhere real instead of the not-found placeholder.
    it('redirects /transactions/new to the transactions list', async () => {
      const router = createMemoryRouter(routes, {
        initialEntries: ['/transactions/new'],
      })
      render(
        <ThemeProvider>
          <RouterProvider router={router} />
        </ThemeProvider>,
      )

      expect(
        await screen.findByRole('heading', { name: 'Transactions' }),
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
      render(
        <ThemeProvider>
          <RouterProvider router={router} />
        </ThemeProvider>,
      )

      expect(
        await screen.findByRole('heading', { name: 'Page not found' }),
      ).toBeInTheDocument()
    })
  })

  it('renders RouteError instead of the default crash screen when a route throws', async () => {
    // React and react-router-dom both log a thrown render error to
    // console.error even once an errorElement catches it — expected here
    // (that's exactly what this test deliberately triggers), but it would
    // otherwise print a full stack trace on every `make test` run for a
    // case that's supposed to pass. Silenced for just this case, and
    // restored immediately after so a genuinely unexpected console.error
    // from any other test still fails loudly.
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})

    function Throws(): never {
      throw new Error('boom')
    }
    const throwingRoutes: RouteObject[] = [
      { path: '/', element: <Throws />, errorElement: <RouteError /> },
    ]
    const router = createMemoryRouter(throwingRoutes, {
      initialEntries: ['/'],
    })
    render(
      <ThemeProvider>
        <RouterProvider router={router} />
      </ThemeProvider>,
    )

    try {
      expect(
        await screen.findByRole('heading', { name: 'Something went wrong' }),
      ).toBeInTheDocument()
    } finally {
      consoleError.mockRestore()
    }
  })
})
