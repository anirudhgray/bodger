import { useState } from 'react'
import {
  ArrowLeftRight,
  BarChart3,
  ChevronRight,
  Moon,
  PiggyBank,
  Receipt,
  Settings as SettingsIcon,
  Sun,
  Wallet,
} from 'lucide-react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
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
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  SidebarTrigger,
  useSidebar,
} from '@/components/ui/sidebar'
import { TransactionDialogProvider } from '@/components/TransactionDialog'
import { useTransactionDialog } from '@/hooks/use-transaction-dialog'
import { useTheme } from '@/hooks/use-theme'
import { logout } from '@/lib/session'
import { IMPORT_EXPORT_SECTIONS } from '@/pages/data/shared'
import { SETTINGS_SECTIONS } from '@/pages/settings/shared'

// Each item's icon matches the one its own screen already uses for its
// empty state (TransactionsList.tsx's Receipt, Balances.tsx's Wallet,
// Analytics.tsx's BarChart3, Budgets.tsx's PiggyBank) — reusing that
// vocabulary rather than picking new icons for the same concept. Import &
// export and Settings are both rendered separately below, as collapsible
// groups rather than plain links — see AppSidebar's comment for why.
const navItems = [
  { to: '/transactions', label: 'Transactions', icon: Receipt },
  { to: '/balances', label: 'Balances', icon: Wallet },
  { to: '/analytics', label: 'Analytics', icon: BarChart3 },
  { to: '/budgets', label: 'Budgets', icon: PiggyBank },
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

  const isSettingsActive = location.pathname.startsWith('/settings')
  const isImportExportActive = location.pathname.startsWith('/import-export')

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
              {/* Import & export (issue #214) is a collapsible group, the
                  same shape as Settings just below: its three flows
                  (import/export/restore) are also reachable from
                  ImportExportLayout's own tab strip once you're already on
                  one of its pages — this is the second, collapsed way in,
                  matching how Settings offers both. Collapsed by default
                  so it doesn't crowd the top-level nav, opened by default
                  only when one of its own routes is already active. */}
              <Collapsible
                defaultOpen={isImportExportActive}
                className="group/import-export-collapsible"
              >
                <SidebarMenuItem>
                  <CollapsibleTrigger asChild>
                    <SidebarMenuButton isActive={isImportExportActive}>
                      <ArrowLeftRight />
                      Import & export
                      <ChevronRight className="ml-auto transition-transform group-data-[state=open]/import-export-collapsible:rotate-90" />
                    </SidebarMenuButton>
                  </CollapsibleTrigger>
                  <CollapsibleContent>
                    <SidebarMenuSub>
                      {IMPORT_EXPORT_SECTIONS.map((section) => {
                        const isSectionActive = location.pathname === section.to
                        return (
                          <SidebarMenuSubItem key={section.to}>
                            <SidebarMenuSubButton
                              asChild
                              isActive={isSectionActive}
                            >
                              <Link
                                to={section.to}
                                onClick={closeMobileSidebar}
                                aria-current={
                                  isSectionActive ? 'page' : undefined
                                }
                              >
                                {section.label}
                              </Link>
                            </SidebarMenuSubButton>
                          </SidebarMenuSubItem>
                        )
                      })}
                    </SidebarMenuSub>
                  </CollapsibleContent>
                </SidebarMenuItem>
              </Collapsible>
              {/* Settings is a collapsible group rather than a plain link:
                  its four subpages (issue #107's SettingsLayout tab strip)
                  are also reachable straight from the sidebar, collapsed
                  by default so they don't crowd the top-level nav, and
                  opened by default only when a settings route is already
                  active so the sidebar shows where you are. The trigger
                  itself only expands/collapses — it doesn't navigate — so
                  reaching a section always means picking one from the
                  list, the same as the tab strip does. */}
              <Collapsible
                defaultOpen={isSettingsActive}
                className="group/settings-collapsible"
              >
                <SidebarMenuItem>
                  <CollapsibleTrigger asChild>
                    <SidebarMenuButton isActive={isSettingsActive}>
                      <SettingsIcon />
                      Settings
                      <ChevronRight className="ml-auto transition-transform group-data-[state=open]/settings-collapsible:rotate-90" />
                    </SidebarMenuButton>
                  </CollapsibleTrigger>
                  <CollapsibleContent>
                    <SidebarMenuSub>
                      {SETTINGS_SECTIONS.map((section) => {
                        const isSectionActive = location.pathname === section.to
                        return (
                          <SidebarMenuSubItem key={section.to}>
                            <SidebarMenuSubButton
                              asChild
                              isActive={isSectionActive}
                            >
                              <Link
                                to={section.to}
                                onClick={closeMobileSidebar}
                                aria-current={
                                  isSectionActive ? 'page' : undefined
                                }
                              >
                                {section.label}
                              </Link>
                            </SidebarMenuSubButton>
                          </SidebarMenuSubItem>
                        )
                      })}
                    </SidebarMenuSub>
                  </CollapsibleContent>
                </SidebarMenuItem>
              </Collapsible>
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
