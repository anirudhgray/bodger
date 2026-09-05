// shadcn's sonner-based Toaster (https://ui.shadcn.com/docs/components/radix/sonner)
// — the Radix Toast primitive this replaces is deprecated upstream in
// favor of sonner. Adapted from shadcn's next-themes-based reference to
// this app's own ThemeProvider/useTheme (hooks/use-theme.tsx), and themed
// with this app's own CSS variable tokens rather than shadcn's defaults.
//
// Call sites just `import { toast } from 'sonner'` directly (`toast.success(...)`,
// `toast.promise(...)`, etc.) — this file only owns the <Toaster/> itself,
// mounted once at the app root (main.tsx).
import type * as React from 'react'
import { Toaster as Sonner, type ToasterProps } from 'sonner'

import { useTheme } from '@/hooks/use-theme'

function Toaster({ ...props }: ToasterProps) {
  const { theme } = useTheme()

  return (
    <Sonner
      theme={theme}
      className="toaster group"
      position="bottom-right"
      style={
        {
          '--normal-bg': 'var(--popover)',
          '--normal-text': 'var(--popover-foreground)',
          '--normal-border': 'var(--border)',
          '--success-bg': 'var(--popover)',
          '--success-text': 'var(--success)',
          '--success-border': 'var(--border)',
          '--error-bg': 'var(--popover)',
          '--error-text': 'var(--destructive)',
          '--error-border': 'var(--border)',
        } as React.CSSProperties
      }
      {...props}
    />
  )
}

export { Toaster }
