// Routing skeleton (issue #58). Every route below renders a placeholder —
// the screen it belongs to is a separate, later issue (noted per route)
// that replaces the placeholder with the real thing. Nothing here may
// grow financial logic: this file only maps a path to a component, per
// docs/architecture.md §3 ("What the web UI may not do").
import {
  createBrowserRouter,
  isRouteErrorResponse,
  Navigate,
  redirect,
  type RouteObject,
  useRouteError,
} from 'react-router-dom'

import { AppLayout } from '@/App'
import { BalancesPage } from '@/pages/Balances'
import { Login } from '@/pages/Login'
import { Placeholder } from '@/pages/Placeholder'
import { Settings } from '@/pages/Settings'
import { TransactionEntry } from '@/pages/TransactionEntry'
import { TransactionsList } from '@/pages/TransactionsList'
import { checkSession } from '@/lib/session'

// RouteError is exported only so routes.test.tsx can mount it directly
// against a route that deliberately throws — the real route tree below
// never needs to reach for it by name, since every route that wants it
// just writes errorElement: <RouteError />.
//
// RouteError is react-router-dom's errorElement for the whole tree: it
// replaces the library's own default crash screen (which cites its own
// internals — "provide your own ErrorBoundary or errorElement prop", not
// something a bodger user should ever see) with the same Placeholder
// styling every other screen in this file uses. isRouteErrorResponse
// distinguishes a thrown Response (a route the router itself couldn't
// resolve — a raw fetch or navigation failure, not a real path in
// routes.tsx) from an actual thrown error in a component.
export function RouteError() {
  const error = useRouteError()
  const description = isRouteErrorResponse(error)
    ? `${error.status} ${error.statusText}`
    : 'Reload the page, or try again in a moment.'
  return <Placeholder title="Something went wrong" description={description} />
}

// redirectIfAuthenticated is /login's loader (issue #59): visiting the
// login page with a still-valid session cookie sends you straight past
// it rather than asking for a password again.
async function redirectIfAuthenticated() {
  if (await checkSession()) {
    return redirect('/')
  }
  return null
}

// requireAuth is the '/' layout route's loader: it runs before AppLayout
// (and anything nested under it) ever renders, and sends an
// unauthenticated visitor to /login instead — the "route guard/redirect"
// issue #59 asks for. Everything under this layout can assume, without
// checking again itself, that it's rendering for an authenticated actor.
async function requireAuth() {
  if (!(await checkSession())) {
    return redirect('/login')
  }
  return null
}

// routes is the route tree itself, separated from router below so
// routes.test.tsx can mount it in a createMemoryRouter instead — a real
// createBrowserRouter reads and owns actual browser history, which makes
// it awkward to point at more than one starting URL across a test file.
export const routes: RouteObject[] = [
  {
    path: '/login',
    element: <Login />,
    loader: redirectIfAuthenticated,
    errorElement: <RouteError />,
  },
  {
    path: '/',
    element: <AppLayout />,
    loader: requireAuth,
    errorElement: <RouteError />,
    children: [
      { index: true, element: <Navigate to="/transactions" replace /> },
      {
        path: 'transactions',
        // issue #61 — transaction list with filters
        element: <TransactionsList />,
      },
      {
        path: 'transactions/new',
        // issue #60 — fast transaction entry
        element: <TransactionEntry />,
      },
      {
        path: 'balances',
        element: <BalancesPage />,
      },
      {
        path: 'settings',
        // issue #62 — settings: password, API tokens, accounts/categories CRUD
        element: <Settings />,
      },
      {
        // Catches anything that isn't one of the paths above — a typo'd
        // URL, a stale bookmark, and so on. Without this, react-router
        // renders the same default crash screen RouteError exists to
        // replace, for every unmatched path, not just a thrown error.
        path: '*',
        element: (
          <Placeholder
            title="Page not found"
            description="Check the address, or head back to your transactions."
          />
        ),
      },
    ],
  },
]

export const router = createBrowserRouter(routes)
