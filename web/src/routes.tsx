// Routing skeleton (issue #58). Every route below renders a placeholder —
// the screen it belongs to is a separate, later issue (noted per route)
// that replaces the placeholder with the real thing. Nothing here may
// grow financial logic: this file only maps a path to a component, per
// docs/architecture.md §3 ("What the web UI may not do").
import {
  createBrowserRouter,
  Navigate,
  type RouteObject,
} from 'react-router-dom'

import { AppLayout } from '@/App'
import { Placeholder } from '@/pages/Placeholder'

// routes is the route tree itself, separated from router below so
// routes.test.tsx can mount it in a createMemoryRouter instead — a real
// createBrowserRouter reads and owns actual browser history, which makes
// it awkward to point at more than one starting URL across a test file.
export const routes: RouteObject[] = [
  {
    path: '/login',
    // issue #59 — login and session handling
    element: (
      <Placeholder
        title="Log in"
        description="Session-cookie login lands in issue #59."
      />
    ),
  },
  {
    path: '/',
    element: <AppLayout />,
    children: [
      { index: true, element: <Navigate to="/transactions" replace /> },
      {
        path: 'transactions',
        // issue #61 — transaction list with filters
        element: (
          <Placeholder
            title="Transactions"
            description="The filterable transaction list lands in issue #61."
          />
        ),
      },
      {
        path: 'transactions/new',
        // issue #60 — fast transaction entry
        element: (
          <Placeholder
            title="Add a transaction"
            description="Fast transaction entry lands in issue #60."
          />
        ),
      },
      {
        path: 'balances',
        // issue #63 — account balances view
        element: (
          <Placeholder
            title="Balances"
            description="The account balances view lands in issue #63."
          />
        ),
      },
      {
        path: 'settings',
        // issue #62 — settings: password, API tokens, accounts/categories CRUD
        element: (
          <Placeholder
            title="Settings"
            description="Password, API tokens, and accounts/categories management land in issue #62."
          />
        ),
      },
    ],
  },
]

export const router = createBrowserRouter(routes)
