// Shared between the three Import/export/restore subpages (issue #214):
// one section-list definition App.tsx's sidebar submenu reads (the same
// role settings/shared.ts's SETTINGS_SECTIONS plays for Settings — see
// AppSidebar's own comment for why this area is a sidebar submenu rather
// than an in-page tab strip), plus the one bit of logic genuinely
// repeated across all three (error-message extraction) — each subpage
// otherwise has its own upload, review, and confirmation flow, so a
// shared base component would mostly hide that they're different.
import { ApiError } from '@/lib/api'

export function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : 'Couldn’t reach the server. Try again.'
}

export const IMPORT_EXPORT_SECTIONS: {
  to: string
  slug: string
  label: string
}[] = [
  { to: '/import-export/import', slug: 'import', label: 'Import' },
  { to: '/import-export/export', slug: 'export', label: 'Export' },
  { to: '/import-export/restore', slug: 'restore', label: 'Restore' },
]
