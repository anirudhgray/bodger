import * as React from 'react'
import { cn } from 'cn'
import { Progress as ProgressPrimitive } from 'radix-ui'

// indicatorClassName (issue #245) is the one addition on top of shadcn's
// own generated shape: the budgets screen needs the filled portion to
// change color for its under/at/over-budget utilisation states (per
// docs/design-system.md's restricted palette — success/primary/destructive,
// never a one-off color), which the upstream component has no hook for.
// marker (issue #245's month-progress indicator) is the second: an
// optional 0-100 position for a thin reference line drawn over the bar,
// independent of `value` — the budgets screen uses it to mark how far
// through the current period we are, so a line's own fill can be
// compared against it (80% spent on the 29th of a 30-day month reads very
// differently from 80% spent on the 3rd). Both are kept as single prop
// additions on top of the generated component rather than a fork, so a
// future `npx shadcn add progress --overwrite` only needs these two lines
// re-added, not a full rewrite.
function Progress({
  className,
  value,
  indicatorClassName,
  marker,
  ...props
}: React.ComponentProps<typeof ProgressPrimitive.Root> & {
  indicatorClassName?: string
  marker?: number
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
      {marker !== undefined && (
        <div
          data-slot="progress-marker"
          className="bg-foreground/60 absolute top-1/2 h-2.5 w-px -translate-y-1/2"
          style={{ left: `${Math.min(Math.max(marker, 0), 100)}%` }}
        />
      )}
    </ProgressPrimitive.Root>
  )
}

export { Progress }
