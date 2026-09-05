import { useState } from 'react'
import {
  Moon,
  Receipt,
  Settings as SettingsIcon,
  Sun,
  Wallet,
} from 'lucide-react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
  useSidebar,
} from '@/components/ui/sidebar'
import {
  TransactionDialogProvider,
  useTransactionDialog,
} from '@/components/TransactionDialog'
import { useTheme } from '@/hooks/use-theme'
import { logout } from '@/lib/session'

// Each item's icon matches the one its own screen already uses for its
// empty state (TransactionsList.tsx's Receipt, Balances.tsx's Wallet) —
// reusing that vocabulary rather than picking new icons for the same
// concept. Settings has no single equivalent (KeyRound is scoped to its
// API-tokens section specifically), so it gets the generic gear.
const navItems = [
  { to: '/transactions', label: 'Transactions', icon: Receipt },
  { to: '/balances', label: 'Balances', icon: Wallet },
  { to: '/settings', label: 'Settings', icon: SettingsIcon },
]

// AppLayout is the routing skeleton's shell: a sidebar nav, a header, and
// an <Outlet /> for whichever screen the current route renders. Its own
// loader (routes.tsx's requireAuth) already guarantees an authenticated
// actor by the time this renders, so the only auth concern this component
// itself owns is logging out.
export function AppLayout() {
  return (
    <TransactionDialogProvider>
      <SidebarProvider>
        <AppShell />
      </SidebarProvider>
    </TransactionDialogProvider>
  )
}

function AppSidebar() {
  const location = useLocation()
  const { setOpenMobile } = useSidebar()

  // On a narrow viewport the sidebar renders as an off-canvas sheet.
  // Following a nav link navigates but doesn't close the sheet on its
  // own — Radix's Sheet only closes on its own overlay/escape handling,
  // not as a side effect of a click inside it that also happens to
  // navigate — so every link closes it explicitly. Without this, the
  // sheet's overlay is left covering the very page it just navigated to,
  // blocking every control underneath.
  function closeMobileSidebar() {
    setOpenMobile(false)
  }

  return (
    <Sidebar>
      <SidebarHeader>
        <span className="px-2 py-1.5 text-sm font-semibold tracking-tight">
          bodger
        </span>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {navItems.map((item) => {
                const isActive = location.pathname.startsWith(item.to)
                return (
                  <SidebarMenuItem key={item.to}>
                    <SidebarMenuButton asChild isActive={isActive}>
                      <Link
                        to={item.to}
                        onClick={closeMobileSidebar}
                        aria-current={isActive ? 'page' : undefined}
                      >
                        <item.icon />
                        {item.label}
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                )
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  )
}

function AppShell() {
  const navigate = useNavigate()
  const [loggingOut, setLoggingOut] = useState(false)
  const { theme, toggleTheme } = useTheme()
  const { openCreate } = useTransactionDialog()

  function handleAdd() {
    openCreate(() => navigate('/transactions'))
  }

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
    <>
      <AppSidebar />
      <SidebarInset>
        <header className="border-border flex items-center justify-between border-b px-4 py-3 sm:px-6">
          <div className="flex items-center gap-2">
            <SidebarTrigger />
            <span className="text-sm font-semibold tracking-tight md:hidden">
              bodger
            </span>
          </div>
          <div className="flex items-center gap-1">
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground cursor-pointer rounded-md px-3 py-1.5 text-sm transition-colors"
              onClick={handleAdd}
            >
              Add
            </button>
            <Button
              size="icon-sm"
              variant="ghost"
              className="ml-2"
              onClick={toggleTheme}
            >
              {theme === 'dark' ? <Sun /> : <Moon />}
              <span className="sr-only">
                {theme === 'dark'
                  ? 'Switch to light mode'
                  : 'Switch to dark mode'}
              </span>
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={loggingOut}
              onClick={handleLogout}
            >
              {loggingOut ? 'Logging out…' : 'Log out'}
            </Button>
          </div>
        </header>
        <div className="flex flex-1 flex-col">
          <Outlet />
        </div>
      </SidebarInset>
    </>
  )
}
