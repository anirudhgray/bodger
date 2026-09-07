// Shared "fetch rates from the provider" popover — issue #139's
// `pages/Balances.tsx` settled the shape (a button opening a popover
// listing every in-use currency as a checkbox, pre-checked for whichever
// are actually stale or unconverted, firing POST /api/v1/fx/rates/fetch
// only for the ones left checked). Extracted here because issue #145's
// TransactionsList needs the same list-of-checkboxes-plus-confirm shape
// but with one addition Balances never needs: a `from`/`to` date range,
// since Balances always fetches "today" (its `current` policy has no
// other sensible date) while a `transaction_date`-policy screen spans
// many historical dates. See docs/design-system.md's "Refresh popover"
// section.
//
// Fully controlled: every piece of state (open/selected/date range) lives
// in the caller, this only renders the chrome. That keeps each caller's
// own "what should be pre-checked when this opens" logic (which differs:
// Balances checks against its currently-converted view, TransactionsList
// against its currently-loaded page) out of this shared file rather than
// forcing one screen's definition of "needs refresh" onto the other.
import { RefreshCcw } from 'lucide-react'
import type { ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { DatePicker } from '@/components/ui/date-picker'
import { Label } from '@/components/ui/label'
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Spinner } from '@/components/ui/spinner'

export type RateFetchDateRange = {
  from: string
  to: string
  onFromChange: (value: string) => void
  onToChange: (value: string) => void
}

export function RateFetchPopover({
  open,
  onOpenChange,
  triggerLabel,
  title,
  description,
  candidates,
  selected,
  onToggle,
  onConfirm,
  confirming,
  confirmLabel = 'Refresh',
  dateRange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  triggerLabel: ReactNode
  title: string
  description: string
  candidates: string[]
  selected: Set<string>
  onToggle: (currency: string, checked: boolean) => void
  onConfirm: () => void
  confirming: boolean
  confirmLabel?: string
  // Omit for a "today only" fetch (Balances' own popover); pass to add
  // the from/to backfill-range inputs (TransactionsList's).
  dateRange?: RateFetchDateRange
}) {
  const rangeIncomplete = Boolean(
    dateRange && (!dateRange.from || !dateRange.to),
  )

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        <Button type="button" variant="secondary" size="sm">
          <RefreshCcw />
          {triggerLabel}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start">
        <PopoverHeader>
          <PopoverTitle>{title}</PopoverTitle>
          <PopoverDescription>{description}</PopoverDescription>
        </PopoverHeader>
        {dateRange && (
          <div className="flex gap-2 py-1">
            <div className="flex flex-1 flex-col gap-1">
              <Label htmlFor="rate-fetch-from">From</Label>
              <DatePicker
                id="rate-fetch-from"
                value={dateRange.from}
                onChange={dateRange.onFromChange}
              />
            </div>
            <div className="flex flex-1 flex-col gap-1">
              <Label htmlFor="rate-fetch-to">To</Label>
              <DatePicker
                id="rate-fetch-to"
                value={dateRange.to}
                onChange={dateRange.onToChange}
              />
            </div>
          </div>
        )}
        <div className="flex flex-col gap-2 py-1">
          {candidates.map((c) => (
            <label key={c} className="flex items-center gap-2 text-sm">
              <Checkbox
                checked={selected.has(c)}
                onCheckedChange={(checked) => onToggle(c, checked === true)}
              />
              {c}
            </label>
          ))}
        </div>
        <Button
          type="button"
          size="sm"
          disabled={selected.size === 0 || confirming || rangeIncomplete}
          onClick={onConfirm}
        >
          {confirming && <Spinner className="size-3.5" />}
          {confirmLabel}
        </Button>
      </PopoverContent>
    </Popover>
  )
}
