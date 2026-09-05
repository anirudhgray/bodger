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

describe('toast.promise', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows a loading toast, then updates it in place to success', async () => {
    const { result } = renderHook(() => useToast())

    let resolvePromise: (value: string) => void = () => {}
    const pending = new Promise<string>((resolve) => {
      resolvePromise = resolve
    })

    let returned!: Promise<string>
    act(() => {
      returned = toast.promise(pending, {
        loading: 'Recording…',
        success: (value) => `Recorded: ${value}.`,
      })
    })

    expect(result.current.toasts).toHaveLength(1)
    expect(result.current.toasts[0]).toMatchObject({
      title: 'Recording…',
      variant: 'loading',
      version: 0,
    })
    const id = result.current.toasts[0].id

    await act(async () => {
      resolvePromise('tx-1')
      await pending
    })

    // Same toast (same id), updated in place — not a second toast added
    // alongside the first.
    expect(result.current.toasts).toHaveLength(1)
    expect(result.current.toasts[0]).toMatchObject({
      id,
      title: 'Recorded: tx-1.',
      variant: 'success',
      version: 1,
    })

    // toast.promise is a pure side effect — it hands back the same promise
    // it was given, unaltered, so an existing `await`/`try`/`catch` call
    // site keeps working exactly as before.
    await expect(returned).resolves.toBe('tx-1')

    act(() => {
      dismissToast(id)
      vi.advanceTimersByTime(300)
    })
  })

  it('updates the loading toast to an error message when given one, without swallowing the rejection', async () => {
    const { result } = renderHook(() => useToast())

    let rejectPromise: (err: unknown) => void = () => {}
    const pending = new Promise<string>((_resolve, reject) => {
      rejectPromise = reject
    })

    let caught: unknown
    act(() => {
      toast
        .promise(pending, {
          loading: 'Revoking…',
          success: 'Token revoked.',
          error: (err) => (err instanceof Error ? err.message : 'Failed.'),
        })
        .catch((err) => {
          caught = err
        })
    })

    const id = result.current.toasts[0].id

    await act(async () => {
      rejectPromise(new Error('network down'))
      await pending.catch(() => {})
    })

    expect(result.current.toasts).toHaveLength(1)
    expect(result.current.toasts[0]).toMatchObject({
      id,
      title: 'network down',
      variant: 'destructive',
    })
    expect(caught).toBeInstanceOf(Error)

    act(() => {
      dismissToast(id)
      vi.advanceTimersByTime(300)
    })
  })

  it('dismisses the loading toast on rejection when no error message is given', async () => {
    const { result } = renderHook(() => useToast())

    let rejectPromise: (err: unknown) => void = () => {}
    const pending = new Promise<string>((_resolve, reject) => {
      rejectPromise = reject
    })

    act(() => {
      toast
        .promise(pending, { loading: 'Working…', success: 'Done.' })
        .catch(() => {})
    })
    expect(result.current.toasts).toHaveLength(1)

    await act(async () => {
      rejectPromise(new Error('boom'))
      await pending.catch(() => {})
      vi.advanceTimersByTime(300)
    })

    expect(result.current.toasts).toHaveLength(0)
  })
})
