// useTheme wires lib/theme.ts's persisted preference into React (issue
// #101's dark-mode toggle): index.html's inline script already applies
// the resolved theme before paint, so this only needs to bring React's
// own state in sync on mount and re-apply/persist it whenever toggled.
import { useCallback, useEffect, useState } from 'react'

import { applyTheme, persistTheme, resolveTheme, type Theme } from '@/lib/theme'

export function useTheme(): { theme: Theme; toggleTheme: () => void } {
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

  return { theme, toggleTheme }
}
