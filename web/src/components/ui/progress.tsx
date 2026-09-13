import * as React from 'react'
import { cn } from 'cn'
import { Progress as ProgressPrimitive } from 'radix-ui'

// indicatorClassName (issue #245) is the one addition on top of shadcn's
// own generated shape: the budgets screen needs the filled portion to
// change color for its under/at/over-budget utilisation states (per
// docs/design-system.md's restricted palette — success/primary/destructive,
// never a one-off color), which the upstream component has no hook for.
// marker/markerTitle (issue #245's month-progress indicator) are the
// second and third: an optional 0-100 position for a reference line drawn
// over the bar, independent of `value` — the budgets screen uses it to
// mark how far through the current period we are, so a line's own fill
// can be compared against it (80% spent on the 29th of a 30-day month
// reads very differently from 80% spent on the 3rd). The marker is drawn
// taller and wider than the track itself, in the accent color with a
// background-colored ring around it, specifically so it stays legible
// whether it lands over the muted (unfilled) track or over the filled
// indicator in either of its own colors — a same-color marker over a
// same-color fill would otherwise disappear exactly when it matters most
// (spending already past the marker's own position). markerTitle sets a
// native `title` attribute rather than reaching for `components/ui/
// tooltip.tsx`'s Radix Tooltip: docs/design-system.md's "Balances:
// rate-provenance detail row" section already worked through this same
// choice and deliberately avoided Tooltip for a mobile-reachable screen
// (hover-only, no touch equivalent, and it requires an app-wide
// TooltipProvider nothing else in the app has needed yet) — a plain
// `title` costs neither. All three are kept as single prop additions on
// top of the generated component rather than a fork, so a future
// `npx shadcn add progress --overwrite` only needs these lines re-added,
// not a full rewrite.
function Progress({
  className,
  value,
  indicatorClassName,
  marker,
  markerTitle,
  ...props
}: React.ComponentProps<typeof ProgressPrimitive.Root> & {
  indicatorClassName?: string
  marker?: number
  markerTitle?: string
}) {
  return (
    <ProgressPrimitive.Root
      data-slot="progress"
      title={marker !== undefined ? markerTitle : undefined}
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
          className="bg-primary ring-background absolute top-1/2 h-4 w-1 -translate-y-1/2 rounded-full ring-2"
          style={{ left: `${Math.min(Math.max(marker, 0), 100)}%` }}
        />
      )}
    </ProgressPrimitive.Root>
  )
}

export { Progress }
