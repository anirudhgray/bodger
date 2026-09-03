import { useState } from 'react'
import { NavLink, Outlet, useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { logout } from '@/lib/session'

const navItems = [
  { to: '/transactions', label: 'Transactions' },
  { to: '/transactions/new', label: 'Add' },
  { to: '/balances', label: 'Balances' },
  { to: '/settings', label: 'Settings' },
]

// AppLayout is the routing skeleton's shell: a nav bar and an <Outlet />
// for whichever placeholder (soon: real screen) the current route
// renders. Its own loader (routes.tsx's requireAuth) already guarantees
// an authenticated actor by the time this renders, so the only auth
// concern this component itself owns is logging out.
export function AppLayout() {
  const navigate = useNavigate()
  const [loggingOut, setLoggingOut] = useState(false)

  async function handleLogout() {
    setLoggingOut(true)
    try {
      await logout()
    } catch {
      // The route guard on '/' will bounce back here anyway if the
      // session is genuinely still live, but navigating regardless (below)
      // keeps the button responsive rather than stuck disabled on a
      // network error — a bare try/finally would still navigate, but
      // would also leave this handler's own promise rejected with nothing
      // to catch it (an actual unhandled-rejection console error, not
      // just a test artifact).
    } finally {
      navigate('/login', { replace: true })
    }
  }

  return (
    <div className="flex min-h-svh flex-col">
      <header className="border-border flex items-center justify-between border-b px-6 py-3">
        <span className="text-sm font-semibold tracking-tight">bodger</span>
        <nav className="flex items-center gap-1">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              className={({ isActive }) =>
                cn(
                  'rounded-md px-3 py-1.5 text-sm transition-colors',
                  isActive
                    ? 'bg-secondary text-secondary-foreground'
                    : 'text-muted-foreground hover:text-foreground',
                )
              }
            >
              {item.label}
            </NavLink>
          ))}
          <Button
            size="sm"
            variant="outline"
            className="ml-2"
            disabled={loggingOut}
            onClick={handleLogout}
          >
            {loggingOut ? 'Logging out…' : 'Log out'}
          </Button>
        </nav>
      </header>
      <main className="flex flex-1 flex-col">
        <Outlet />
      </main>
    </div>
  )
}
