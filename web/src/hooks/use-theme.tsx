// ThemeProvider/useTheme wire lib/theme.ts's persisted preference into
// React (issue #101's dark-mode toggle) via context, not a bare hook with
// its own local state: a bare hook gives every call site an independent
// useState, so a second consumer (a future settings-page toggle, a
// chart's theme-aware colors) would silently desync from whichever one
// last called toggleTheme — the DOM class and localStorage would agree,
// but each hook instance's own `theme` value wouldn't, since nothing
// connects them. Context makes the app's theme what it actually is: one
// value, shared. index.html's inline script still applies the resolved
// theme before paint (see lib/theme.ts) — this only needs to bring
// React's own state in sync on mount and re-apply/persist it when
// toggled. Matches shadcn's own recommended Vite pattern:
// https://ui.shadcn.com/docs/dark-mode/vite
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react'

import { applyTheme, persistTheme, resolveTheme, type Theme } from '@/lib/theme'

interface ThemeContextValue {
  theme: Theme
  toggleTheme: () => void
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(() => resolveTheme())

  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  const toggleTheme = useCallback(() => {
    setTheme((current) => {
      const next: Theme = current === 'dark' ? 'light' : 'dark'
      persistTheme(next)
      return next
    })
  }, [])

  return (
    <ThemeContext.Provider value={{ theme, toggleTheme }}>
      {children}
    </ThemeContext.Provider>
  )
}

// useTheme is this context's own accessor, not a component; splitting it
// into a second file would just add indirection for shadcn's own
// recommended co-located provider+hook pattern cited above.
// oxlint-disable-next-line react/only-export-components
export function useTheme(): ThemeContextValue {
  const context = useContext(ThemeContext)
  if (context === null) {
    throw new Error('useTheme must be used within a ThemeProvider')
  }
  return context
}
