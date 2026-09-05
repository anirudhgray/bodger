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
