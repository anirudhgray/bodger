// Shared pieces of the click-to-expand rate-provenance row issue #139
// settled on in `pages/Balances.tsx` (see docs/design-system.md's
// "Balances: rate-provenance detail row" section for why this is a
// Collapsible, not a tooltip). Extracted here so issue #145's
// TransactionsList doesn't re-solve the same interaction, and so #141's
// transfer-row provenance reuses this same pattern instead of
// rediscovering it.
//
// Deliberately just the trigger button and the detail container, not a
// bundled Root+Trigger+Content unit: a caller's Collapsible needs to wrap
// its *entire* row (so the expanded detail lands full-width below it, the
// way Balances' <li> and TransactionsList's <li> both need), while the
// trigger itself sits nested deep inside that row next to the amount —
// Radix's Collapsible.Trigger/Content only need to share a Root via
// context, not be DOM-adjacent, so callers import `Collapsible` directly
// from `components/ui/collapsible` for that Root and drop these two
// pieces wherever their own layout needs them. Each caller also writes
// its own detail sentence rather than this component prescribing one —
// #141's transfer rows have no `rate_date`/staleness to report at all
// (a transfer's rate is fixed at write time, never stale), so a shared
// sentence shape would have to special-case that anyway.
import type { ReactNode } from 'react'

import {
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'

// RateAmountTrigger is the "≈ 1380.00 EUR" (+ optional inline "stale"
// flag) button that expands the detail row below it. `children` is the
// converted-amount label; formatting/currency codes are the caller's own
// concern (a transaction row and an account row don't necessarily read
// the amount the same way).
export function RateAmountTrigger({
  children,
  stale,
}: {
  children: ReactNode
  stale?: boolean
}) {
  return (
    <CollapsibleTrigger asChild>
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs tabular-nums underline decoration-dotted underline-offset-4"
      >
        <span>{children}</span>
        {stale && <span className="text-destructive font-medium">stale</span>}
      </button>
    </CollapsibleTrigger>
  )
}

// RateProvenanceDetail is the full-sentence row that expands underneath
// ("1 USD = 0.92 EUR · as of 2026-09-03 · frankfurter"). `children` is the
// whole sentence — see the note above on why this doesn't prescribe its
// shape itself.
export function RateProvenanceDetail({ children }: { children: ReactNode }) {
  return (
    <CollapsibleContent className="border-border text-muted-foreground border-t px-4 py-2 text-xs">
      {children}
    </CollapsibleContent>
  )
}

// UnconvertedNote is the sibling "not converted" flag for a row a
// conversion couldn't cover at all (ADR-0004's mixed-policy-aggregate-
// forbidden rule: never silently drop or blank such a row). Not a
// disclosure — there's no provenance to show for a conversion that never
// happened.
export function UnconvertedNote({ reason }: { reason?: string }) {
  return (
    <span className="text-destructive text-xs">
      Not converted{reason ? ` — ${reason}` : ''}
    </span>
  )
}
