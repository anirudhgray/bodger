// The module-level store (not React state) is the part with real logic
// worth a direct test: toast() has to notify every subscriber, and
// dismissToast() has to mark the toast closed immediately but only
// actually drop it from the list after the close-animation delay, so a
// closing toast still exists for tw-animate-css's exit animation to run
// against (see REMOVE_DELAY_MS's comment in use-toast.ts).
import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { dismissToast, toast, useToast } from './use-toast'

describe('toast store', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('notifies subscribers when a toast is added', () => {
    const { result } = renderHook(() => useToast())

    let id = ''
    act(() => {
      id = toast({ description: 'Saved.' })
    })

    expect(result.current.toasts).toHaveLength(1)
    expect(result.current.toasts[0]).toMatchObject({
      id,
      description: 'Saved.',
      variant: 'default',
      open: true,
    })

    act(() => {
      dismissToast(id)
      vi.advanceTimersByTime(300)
    })
  })

  it('marks a toast closed immediately, then drops it after the removal delay', () => {
    const { result } = renderHook(() => useToast())

    let id = ''
    act(() => {
      id = toast({ description: 'Deleted.' })
    })
    expect(result.current.toasts).toHaveLength(1)

    act(() => {
      dismissToast(id)
    })
    expect(result.current.toasts).toHaveLength(1)
    expect(result.current.toasts[0].open).toBe(false)

    act(() => {
      vi.advanceTimersByTime(299)
    })
    expect(result.current.toasts).toHaveLength(1)

    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(result.current.toasts).toHaveLength(0)
  })

  it('defaults an unspecified variant/duration and honors an explicit one', () => {
    const { result } = renderHook(() => useToast())

    let id = ''
    act(() => {
      id = toast({
        description: 'Couldn’t reach the server.',
        variant: 'destructive',
        duration: 1000,
      })
    })

    expect(result.current.toasts[0]).toMatchObject({
      variant: 'destructive',
      duration: 1000,
    })

    act(() => {
      dismissToast(id)
      vi.advanceTimersByTime(300)
    })
  })
})
