// Dark mode (issue #101): docs/design-system.md ships a full `.dark`
// token set, but until now nothing let a person choose it — the page
// only ever followed the OS/browser's prefers-color-scheme. This adds a
// real, persisted preference that wins once set, applied consistently
// wherever the theme needs to be read or changed (index.html's inline
// script for the pre-paint case, hooks/use-theme.ts's useTheme for
// React) rather than each place reimplementing its own resolution.
export type Theme = 'light' | 'dark'

const THEME_KEY = 'bodger:theme'

function prefersDark(): boolean {
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}

// storedTheme reads the persisted preference, if any is set. A private
// window or blocked site data throws here — that's not worth failing
// over, so this just reports "no preference stored" instead.
export function storedTheme(): Theme | null {
  try {
    const value = localStorage.getItem(THEME_KEY)
    return value === 'light' || value === 'dark' ? value : null
  } catch {
    return null
  }
}

// resolveTheme is the one place "what theme should this page be in right
// now" gets decided: the stored preference if one exists, otherwise the
// OS/browser's prefers-color-scheme. index.html's inline script and
// useTheme's initial state both call this so a fresh page load and
// React's own state agree without either re-deriving the rule.
export function resolveTheme(): Theme {
  return storedTheme() ?? (prefersDark() ? 'dark' : 'light')
}

// persistTheme stores an explicit choice so it outlives this tab and
// wins over prefers-color-scheme on every later visit. Failing silently
// on a blocked localStorage mirrors storedTheme above — the toggle still
// works for the rest of this session, it just won't be remembered.
export function persistTheme(theme: Theme): void {
  try {
    localStorage.setItem(THEME_KEY, theme)
  } catch {
    // Losing the persisted preference isn't worth failing the toggle over.
  }
}

// applyTheme flips the `.dark` class index.css's `.dark` block (and
// every primitive's `dark:` variant) keys off.
export function applyTheme(theme: Theme): void {
  document.documentElement.classList.toggle('dark', theme === 'dark')
}
