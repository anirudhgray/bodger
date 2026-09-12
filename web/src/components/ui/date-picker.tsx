import * as React from 'react'
import { cn } from 'cn'

import { Skeleton } from '@/components/ui/skeleton'
import type { DatePickerProps } from './date-picker-impl'

// react-day-picker + date-fns pull ~180 KB (unminified) into the bundle
// (issue #119) and are only ever needed once a date field actually
// renders — deferred here so every DatePicker caller gets the split for
// free rather than repeating this at each call site.
//
// This uses a plain useEffect + useState dynamic import rather than
// React.lazy/Suspense: react-dom@19's Suspense retry never flushed in
// this project's jsdom + Vitest test environment (the resolved import
// never triggered a re-render, even with a many-second timeout), while
// a manual effect-driven import resolves immediately and behaves like
// every other async-load-then-setState effect already in this codebase
// (e.g. TransactionsList.tsx's own data-loading effect).
let cachedImpl: React.ComponentType<DatePickerProps> | null = null

function DatePicker(props: DatePickerProps) {
  const [Impl, setImpl] = React.useState(() => cachedImpl)

  React.useEffect(() => {
    if (Impl) return
    let cancelled = false
    import('./date-picker-impl').then((mod) => {
      cachedImpl = mod.DatePicker
      if (!cancelled) setImpl(() => mod.DatePicker)
    })
    return () => {
      cancelled = true
    }
  }, [Impl])

  if (!Impl) {
    return (
      <Skeleton
        className={cn('h-8 w-full', props.className)}
        aria-hidden="true"
      />
    )
  }

  return <Impl {...props} />
}

export { DatePicker }
export type { DatePickerProps }
