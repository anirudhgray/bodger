// The classic pre-sonner shadcn toast pattern: a module-level store, not
// per-component state, since toast() has to be callable from a plain
// event handler (a delete button's onClick, an async success callback)
// that isn't itself a component subscribed to anything. useToast() is
// only what <Toaster/> (components/ui/toast.tsx) uses to render the
// current list; call sites just call toast().
import { useEffect, useState } from 'react'

export type ToastVariant = 'default' | 'success' | 'destructive'

export interface ToastItem {
  id: string
  title?: string
  description?: string
  variant: ToastVariant
  duration: number
  open: boolean
}

type ToastInput = Omit<ToastItem, 'id' | 'open' | 'variant' | 'duration'> & {
  variant?: ToastVariant
  duration?: number
}

const DEFAULT_DURATION = 5000

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

export function toast(input: ToastInput): string {
  const id = crypto.randomUUID()
  toasts = [
    ...toasts,
    {
      id,
      open: true,
      variant: input.variant ?? 'default',
      duration: input.duration ?? DEFAULT_DURATION,
      title: input.title,
      description: input.description,
    },
  ]
  emit()
  return id
}

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
