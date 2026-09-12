// Routing skeleton (issue #58). Every route below renders a placeholder —
// the screen it belongs to is a separate, later issue (noted per route)
// that replaces the placeholder with the real thing. Nothing here may
// grow financial logic: this file only maps a path to a component, per
// docs/architecture.md §3 ("What the web UI may not do").
import {
  createBrowserRouter,
  Navigate,
  redirect,
  type RouteObject,
} from 'react-router-dom'

import { AppLayout } from '@/App'
import { AnalyticsPage } from '@/pages/Analytics'
import { BalancesPage } from '@/pages/Balances'
import { ExportPage } from '@/pages/data/Export'
import { ImportExportLayout } from '@/pages/data/ImportExportLayout'
import { ImportPage } from '@/pages/data/Import'
import { RestorePage } from '@/pages/data/Restore'
import { Login } from '@/pages/Login'
import { Placeholder } from '@/pages/Placeholder'
import { AccountsSettings } from '@/pages/settings/Accounts'
import { ApiTokensSettings } from '@/pages/settings/ApiTokens'
import { CategoriesSettings } from '@/pages/settings/Categories'
import { CurrencySettings } from '@/pages/settings/Currency'
import { PasswordSettings } from '@/pages/settings/Password'
import { SettingsLayout } from '@/pages/settings/SettingsLayout'
import { TransactionsList } from '@/pages/TransactionsList'
import { checkSession } from '@/lib/session'
import { RouteError } from './RouteError'

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
        // transactions/new used to be its own screen (issue #60) — it's
        // now TransactionDialog, opened from anywhere via the nav's Add
        // button rather than navigated to, but an old bookmark or link
        // to this path should still land somewhere real rather than
        // the not-found placeholder below.
        path: 'transactions/new',
        element: <Navigate to="/transactions" replace />,
      },
      {
        path: 'balances',
        element: <BalancesPage />,
      },
      {
        // issue #189 — analytics and charts screen
        path: 'analytics',
        element: <AnalyticsPage />,
      },
      {
        // issue #214 — import wizard, export/backup, and restore-from-
        // backup flows. Mirrors 'settings' below exactly: ImportExportLayout
        // provides the shared heading and tab-strip sub-nav, and App.tsx's
        // AppSidebar additionally offers the same three sections as a
        // collapsible submenu — the same two ways in Settings already has.
        path: 'import-export',
        element: <ImportExportLayout />,
        children: [
          { index: true, element: <Navigate to="import" replace /> },
          { path: 'import', element: <ImportPage /> },
          { path: 'export', element: <ExportPage /> },
          { path: 'restore', element: <RestorePage /> },
        ],
      },
      {
        path: 'settings',
        // issue #62 — settings: password, API tokens, accounts/categories
        // CRUD. issue #107 split the single stacked page into these
        // nested subroutes, with SettingsLayout providing the shared
        // heading and sub-nav (see that file's own comment for why a tab
        // strip, not a second sidebar).
        element: <SettingsLayout />,
        children: [
          { index: true, element: <Navigate to="password" replace /> },
          { path: 'password', element: <PasswordSettings /> },
          { path: 'tokens', element: <ApiTokensSettings /> },
          { path: 'accounts', element: <AccountsSettings /> },
          { path: 'categories', element: <CategoriesSettings /> },
          { path: 'currency', element: <CurrencySettings /> },
        ],
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
