import { NavLink, Outlet } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

const navItems = [
  { to: '/transactions', label: 'Transactions' },
  { to: '/transactions/new', label: 'Add' },
  { to: '/balances', label: 'Balances' },
  { to: '/settings', label: 'Settings' },
]

// AppLayout is the routing skeleton's shell: a nav bar and an <Outlet />
// for whichever placeholder (soon: real screen) the current route
// renders. It holds no application state and calls no API — every screen
// underneath it owns its own data fetching once issues #59-#63 build it.
export function AppLayout() {
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
          <Button asChild size="sm" variant="outline" className="ml-2">
            <NavLink to="/login">Log in</NavLink>
          </Button>
        </nav>
      </header>
      <main className="flex flex-1 flex-col">
        <Outlet />
      </main>
    </div>
  )
}
