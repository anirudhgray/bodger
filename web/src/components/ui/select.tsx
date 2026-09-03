import * as React from 'react'

import { cn } from '@/lib/utils'

// A plain native <select>, styled to match Input (components/ui/input.tsx)
// rather than a Radix-based combobox — this screen's pickers (account,
// category) are short, flat lists with no search need, so the extra
// dependency isn't earned yet. Swap for a Radix Select if a later screen's
// list grows long enough to need one.
function Select({
  className,
  children,
  ...props
}: React.ComponentProps<'select'>) {
  return (
    <select
      data-slot="select"
      className={cn(
        'border-input bg-background flex h-8 w-full min-w-0 rounded-lg border px-2.5 text-sm shadow-xs transition-[color,box-shadow] outline-none',
        'focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50',
        'aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40',
        'disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    >
      {children}
    </select>
  )
}

export { Select }
