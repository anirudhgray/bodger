// ImportExportLayout is the '/import-export' layout route (issue #214):
// the "Import & export" heading and a sub-nav specific to this area, with
// an <Outlet /> below it for whichever subpage (import/export/restore —
// routes.tsx) is currently selected. Mirrors settings/SettingsLayout.tsx's
// own tab-strip pattern exactly — see that file's comment for why a
// horizontal Tabs strip of real <Link>s, not a nested sidebar. This area
// also has its own collapsible submenu in App.tsx's AppSidebar (the same
// shape as Settings' own submenu there) — the two aren't redundant, they're
// the same two ways in Settings already offers: expand it from the
// sidebar without visiting the page, or land on the page and switch tabs
// once there.
import { Link, Outlet, useLocation } from 'react-router-dom'

import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { IMPORT_EXPORT_SECTIONS } from '@/pages/data/shared'

export function ImportExportLayout() {
  const location = useLocation()
  const activeSection =
    location.pathname.split('/').filter(Boolean).pop() ??
    IMPORT_EXPORT_SECTIONS[0].slug

  return (
    <div className="flex flex-1 flex-col gap-6 p-4 sm:p-6">
      <h1 className="text-2xl font-semibold tracking-tight">
        Import &amp; export
      </h1>
      <Tabs value={activeSection}>
        <div className="-mx-1 overflow-x-auto px-1">
          <TabsList
            aria-label="Import and export sections"
            className="w-fit min-w-full justify-start sm:min-w-0"
          >
            {IMPORT_EXPORT_SECTIONS.map((section) => (
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
