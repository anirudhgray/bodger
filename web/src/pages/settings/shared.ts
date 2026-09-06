// Shared between every settings subpage (issue #107 split Settings.tsx's
// four stacked sections into their own routes — see SettingsLayout.tsx for
// the sub-nav that ties them together). Kept to the one genuinely
// repeated bit of logic rather than a speculative shared base component:
// each subpage otherwise has its own form fields, list shape, and
// mutation calls, so a shared "section" wrapper would mostly just hide
// that they're different.
import { ApiError } from '@/lib/api'

export function errorMessage(err: unknown): string {
  return err instanceof ApiError
    ? err.message
    : 'Couldn’t reach the server. Try again.'
}

// The one list every Settings entry point needs: SettingsLayout's own
// tab strip, and App.tsx's sidebar submenu (a second, collapsed way to
// reach the same four routes — see AppSidebar's comment for why both
// exist). `to` is absolute so either caller can use it as-is regardless
// of where in the tree it renders; `slug` is `to`'s own last segment,
// spelled out rather than derived via `.split('/').pop()` at each call
// site — that's typed `string | undefined` since TypeScript can't see
// the array is never empty.
export const SETTINGS_SECTIONS: { to: string; slug: string; label: string }[] =
  [
    { to: '/settings/password', slug: 'password', label: 'Password' },
    { to: '/settings/tokens', slug: 'tokens', label: 'API tokens' },
    { to: '/settings/accounts', slug: 'accounts', label: 'Accounts' },
    { to: '/settings/categories', slug: 'categories', label: 'Categories' },
    { to: '/settings/currency', slug: 'currency', label: 'Currency' },
  ]
