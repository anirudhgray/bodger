import * as React from 'react'
import { cn } from 'cn'
import { Progress as ProgressPrimitive } from 'radix-ui'

// indicatorClassName (issue #245) is the one addition on top of shadcn's
// own generated shape: the budgets screen needs the filled portion to
// change color for its under/at/over-budget utilisation states (per
// docs/design-system.md's restricted palette — success/primary/destructive,
// never a one-off color), which the upstream component has no hook for.
// Kept as a single optional prop rather than a fork so a future
// `npx shadcn add progress --overwrite` only needs this one line re-added,
// not a full rewrite.
function Progress({
  className,
  value,
  indicatorClassName,
  ...props
}: React.ComponentProps<typeof ProgressPrimitive.Root> & {
  indicatorClassName?: string
}) {
  return (
    <ProgressPrimitive.Root
      data-slot="progress"
      className={cn(
        'relative flex h-1 w-full items-center overflow-x-hidden rounded-full bg-muted',
        className,
      )}
      {...props}
    >
      <ProgressPrimitive.Indicator
        data-slot="progress-indicator"
        className={cn(
          'size-full flex-1 bg-primary transition-all',
          indicatorClassName,
        )}
        style={{ transform: `translateX(-${100 - (value || 0)}%)` }}
      />
    </ProgressPrimitive.Root>
  )
}

export { Progress }
