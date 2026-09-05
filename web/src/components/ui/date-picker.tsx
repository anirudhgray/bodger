import * as React from 'react'
import { format, isValid, parse } from 'date-fns'
import { CalendarIcon } from 'lucide-react'
import { cn } from 'cn'

import { Button } from '@/components/ui/button'
import { Calendar } from '@/components/ui/calendar'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

// The date value this app passes around end-to-end (API request bodies,
// TransactionListFilter) is a plain 'yyyy-MM-dd' string, not a Date — so
// this component's public contract stays a string in, string out, with
// Date only ever existing as react-day-picker's own internal
// representation. Parsing/formatting with date-fns (already a
// dependency) rather than `new Date(str)` avoids that constructor's
// UTC-midnight parsing, which shifts the displayed day in negative UTC
// offsets.
const DATE_FORMAT = 'yyyy-MM-dd'

function parseDateValue(value: string): Date | undefined {
  if (!value) return undefined
  const parsed = parse(value, DATE_FORMAT, new Date())
  return isValid(parsed) ? parsed : undefined
}

interface DatePickerProps {
  id?: string
  placeholder?: string
  className?: string
  'aria-label'?: string
  // Controlled mode (TransactionDialog's date field, alongside its
  // other useState-backed fields): pass value+onChange, omit name.
  value?: string
  onChange?: (value: string) => void
  // Uncontrolled mode, for TransactionsList's plain <form>+FormData
  // filter panel, where every other filter field (the Selects) is also
  // read via FormData on submit rather than tracked in per-field state:
  // pass name (+ optional defaultValue), omit value/onChange. A hidden
  // input mirrors the picked value so FormData(form) picks it up the
  // same way it does a native <input type="date">'s value — this
  // component's only real difference from that native input is what
  // opens when you click it.
  name?: string
  defaultValue?: string
}

function DatePicker({
  id,
  placeholder = 'Pick a date',
  className,
  value,
  onChange,
  name,
  defaultValue,
  ...rest
}: DatePickerProps) {
  const [open, setOpen] = React.useState(false)
  const [internalValue, setInternalValue] = React.useState(defaultValue ?? '')
  const isControlled = value !== undefined
  const currentValue = isControlled ? value : internalValue
  const selected = parseDateValue(currentValue)

  function handleSelect(date: Date | undefined) {
    const next = date ? format(date, DATE_FORMAT) : ''
    if (isControlled) {
      onChange?.(next)
    } else {
      setInternalValue(next)
    }
    setOpen(false)
  }

  return (
    <>
      {name && <input type="hidden" name={name} value={currentValue} />}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            id={id}
            type="button"
            variant="outline"
            className={cn(
              'w-full justify-start font-normal',
              !selected && 'text-muted-foreground',
              className,
            )}
            aria-label={rest['aria-label']}
          >
            <CalendarIcon className="opacity-60" />
            {selected ? format(selected, 'PP') : placeholder}
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-auto p-0" align="start">
          <Calendar
            mode="single"
            selected={selected}
            defaultMonth={selected}
            onSelect={handleSelect}
            autoFocus
          />
        </PopoverContent>
      </Popover>
    </>
  )
}

export { DatePicker }
