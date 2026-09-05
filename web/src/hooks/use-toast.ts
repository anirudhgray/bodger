// The classic pre-sonner shadcn toast pattern: a module-level store, not
// per-component state, since toast() has to be callable from a plain
// event handler (a delete button's onClick, an async success callback)
// that isn't itself a component subscribed to anything. useToast() is
// only what <Toaster/> (components/ui/toast.tsx) uses to render the
// current list; call sites just call toast().
import { useEffect, useState } from 'react'

export type ToastVariant = 'default' | 'success' | 'destructive' | 'loading'

export interface ToastItem {
  id: string
  title?: string
  description?: string
  variant: ToastVariant
  duration: number
  open: boolean
  // Bumped on every in-place update (see updateToast). Toaster keys each
  // <Toast/> on `${id}-${version}` so an update forces a remount — Radix's
  // own auto-dismiss timer starts from when its Root mounts, so a loading
  // toast that flips to success/error via toast.promise gets a fresh
  // countdown for its new content instead of inheriting however much of
  // the loading toast's (typically much longer) duration had elapsed.
  version: number
}

type ToastInput = Omit<
  ToastItem,
  'id' | 'open' | 'variant' | 'duration' | 'version'
> & {
  variant?: ToastVariant
  duration?: number
}

const DEFAULT_DURATION = 5000
// Loading toasts track a promise of unknown length — auto-dismissing one
// while its request is still in flight would be misleading, so give it an
// effectively-infinite duration; toast.promise always replaces it with a
// normal-duration success/error toast once the promise settles.
const LOADING_DURATION = 2 ** 31 - 1

// How long tw-animate-css's exit animation (fade-out/slide-out, see
// toast.tsx's toastVariants) takes to finish before the toast is actually
// dropped from the list — matches the other Radix primitives' ~150-200ms
// durations in this codebase, with slack for the animation to visibly
// complete rather than get cut off mid-fade.
const REMOVE_DELAY_MS = 300

let toasts: ToastItem[] = []
const listeners = new Set<(toasts: ToastItem[]) => void>()

function emit() {
  for (const listener of listeners) listener(toasts)
}

function removeToast(id: string) {
  toasts = toasts.filter((t) => t.id !== id)
  emit()
}

function setToastOpen(id: string, open: boolean) {
  toasts = toasts.map((t) => (t.id === id ? { ...t, open } : t))
  emit()
  if (!open) {
    setTimeout(() => removeToast(id), REMOVE_DELAY_MS)
  }
}

function toastBase(input: ToastInput): string {
  const id = crypto.randomUUID()
  toasts = [
    ...toasts,
    {
      id,
      open: true,
      version: 0,
      variant: input.variant ?? 'default',
      duration: input.duration ?? DEFAULT_DURATION,
      title: input.title,
      description: input.description,
    },
  ]
  emit()
  return id
}

// Patches an existing (still-open) toast's content/variant/duration in
// place, bumping `version` so Toaster remounts it (see ToastItem.version).
function updateToast(
  id: string,
  patch: Partial<
    Pick<ToastItem, 'title' | 'description' | 'variant' | 'duration'>
  >,
) {
  toasts = toasts.map((t) =>
    t.id === id ? { ...t, ...patch, version: t.version + 1 } : t,
  )
  emit()
}

type ToastMessage<T> = string | ((value: T) => string)

function resolveMessage<T>(message: ToastMessage<T> | undefined, value: T) {
  return typeof message === 'function' ? message(value) : message
}

/**
 * Shows a loading toast for the lifetime of `promise`, then updates the
 * same toast in place to a success or error message once it settles —
 * shadcn/sonner's "promise" toast pattern, built on our own Radix-based
 * store rather than sonner (see docs/design-system.md's toast section for
 * why this app stays on one primitive library).
 *
 * Purely a side effect: `promise`'s own resolution/rejection is returned
 * unchanged, so existing `await`/`try`/`catch` call sites keep working —
 * this only decorates them with toast lifecycle, it doesn't swallow
 * errors or need its own error handling at the call site.
 */
function promiseToast<T>(
  promise: Promise<T>,
  opts: {
    loading: string
    success: ToastMessage<T>
    error?: ToastMessage<unknown>
  },
): Promise<T> {
  const id = toastBase({
    title: opts.loading,
    variant: 'loading',
    duration: LOADING_DURATION,
  })
  promise.then(
    (value) => {
      updateToast(id, {
        title: resolveMessage(opts.success, value),
        variant: 'success',
        duration: DEFAULT_DURATION,
      })
    },
    (err: unknown) => {
      if (opts.error === undefined) {
        dismissToast(id)
        return
      }
      updateToast(id, {
        title: resolveMessage(opts.error, err),
        variant: 'destructive',
        duration: DEFAULT_DURATION,
      })
    },
  )
  return promise
}

export const toast = Object.assign(toastBase, { promise: promiseToast })

export function dismissToast(id: string) {
  setToastOpen(id, false)
}

export function useToast(): { toasts: ToastItem[] } {
  const [items, setItems] = useState<ToastItem[]>(toasts)

  useEffect(() => {
    listeners.add(setItems)
    return () => {
      listeners.delete(setItems)
    }
  }, [])

  return { toasts: items }
}
