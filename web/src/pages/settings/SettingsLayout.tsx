// SettingsLayout is the '/settings' layout route (issue #107): the
// "Settings" heading and a sub-nav specific to this area, with an
// <Outlet /> below it for whichever subpage (password/tokens/accounts/
// categories — routes.tsx) is currently selected. It replaces the old
// Settings.tsx, which stacked all four sections vertically on one page.
//
// The sub-nav reuses the `tabs` primitive (components/ui/tabs.tsx)
// rather than a second, nested `sidebar`: #88 already settled AppLayout's
// own top-level nav as a full off-canvas Sidebar, and a second Sidebar
// nested inside it for four flat, non-hierarchical items would mean two
// independent collapse/toggle affordances stacked on each other for no
// real benefit — these four sections have no further nesting of their
// own to justify it (unlike accounts/categories' internal lists, which
// aren't navigation). A horizontal tab strip is the boring, proportional
// choice, and it's the same primitive design-system.md already documents
// as "the honest semantics for mutually exclusive views" (TransactionEntry's
// Spend/Receive/Move switch) - `TabsTrigger`'s `asChild` prop lets each tab
// render as a real <Link>, so this is genuine navigation (address bar,
// back button, and bookmarks all work), not content swapped in place.
// The one tradeoff: Radix's tablist/tab roles are written for
// show/hide-in-place panels, not cross-page navigation, so a screen
// reader announces this as tab selection rather than "leaving this page"
// - accepted here since the visited link still lands on a real,
// independently reachable route.
import { Link, Outlet, useLocation } from 'react-router-dom'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SETTINGS_SECTIONS } from '@/pages/settings/shared'

export function SettingsLayout() {
  const location = useLocation()
  const activeSection =
    location.pathname.split('/').filter(Boolean).pop() ??
    SETTINGS_SECTIONS[0].slug

  return (
    <div className="flex flex-1 flex-col gap-6 p-4 sm:p-6">
      <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
      <Tabs value={activeSection}>
        <div className="-mx-1 overflow-x-auto px-1">
          <TabsList
            variant="line"
            aria-label="Settings sections"
            className="w-fit min-w-full justify-start sm:min-w-0"
          >
            {SETTINGS_SECTIONS.map((section) => (
              <TabsTrigger key={section.to} value={section.slug} asChild>
                <Link to={section.to}>{section.label}</Link>
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
      </Tabs>
      <Outlet />
    </div>
  )
}
