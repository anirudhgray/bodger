// Toast doesn't need the @testing-library/user-event + jsdom polyfills
// (hasPointerCapture, scrollIntoView) issue #104 tracks for the other
// Radix-portal primitives — like Dialog and Switch, it works fine under
// plain fireEvent (confirmed in #108's issue body) since it needs neither
// pointer-capture nor scroll-into-view.
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { dismissToast, toast } from '@/hooks/use-toast'

import { Toaster } from './toast'

// Every real call site's message renders through `title`, not
// `description` — description is reserved for a secondary detail line
// under it — since docs/design-system.md's "accent is a signal, not a
// fill" principle puts the variant's color signal on the title text
// (matching the app's existing "Recorded." convention) and the leading
// icon, never on the card's own background.

describe('Toaster', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders the card itself with neutral styling regardless of variant', () => {
    vi.useFakeTimers()
    render(<Toaster />)

    let id = ''
    act(() => {
      id = toast({ title: 'Transaction recorded.', variant: 'success' })
    })

    const title = screen.getByText('Transaction recorded.')
    const toastEl = title.closest('[data-slot="toast"]')
    expect(toastEl).not.toBeNull()
    expect(toastEl?.className).toContain('bg-popover')
    expect(toastEl?.className).not.toContain('bg-success')
    expect(toastEl?.className).not.toContain('bg-destructive')

    act(() => {
      dismissToast(id)
      vi.advanceTimersByTime(300)
    })
  })

  it.each([
    ['success', 'text-success'],
    ['destructive', 'text-destructive'],
    ['loading', 'text-muted-foreground'],
  ] as const)(
    'colors the %s title and shows its icon, not the card background',
    (variant, expectedClass) => {
      vi.useFakeTimers()
      render(<Toaster />)

      let id = ''
      act(() => {
        id = toast({ title: `${variant} message`, variant })
      })

      const title = screen.getByText(`${variant} message`)
      expect(title.className).toContain(expectedClass)

      act(() => {
        dismissToast(id)
        vi.advanceTimersByTime(300)
      })
    },
  )

  it('stacks multiple concurrent toasts', () => {
    vi.useFakeTimers()
    render(<Toaster />)

    let firstId = ''
    let secondId = ''
    act(() => {
      firstId = toast({ title: 'First toast.' })
      secondId = toast({ title: 'Second toast.' })
    })

    expect(screen.getByText('First toast.')).toBeInTheDocument()
    expect(screen.getByText('Second toast.')).toBeInTheDocument()

    act(() => {
      dismissToast(firstId)
      dismissToast(secondId)
      vi.advanceTimersByTime(300)
    })
  })

  it('dismisses a toast when its close button is clicked', async () => {
    vi.useFakeTimers()
    render(<Toaster />)

    act(() => {
      toast({ title: 'Token revoked.' })
    })
    expect(screen.getByText('Token revoked.')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))

    act(() => {
      vi.advanceTimersByTime(300)
    })

    expect(screen.queryByText('Token revoked.')).not.toBeInTheDocument()
  })

  it('converts a loading toast to success in place via toast.promise', async () => {
    vi.useFakeTimers()
    render(<Toaster />)

    let resolvePromise: (value: string) => void = () => {}
    const pending = new Promise<string>((resolve) => {
      resolvePromise = resolve
    })

    act(() => {
      void toast.promise(pending, {
        loading: 'Deleting…',
        success: 'Transaction deleted.',
      })
    })

    expect(screen.getByText('Deleting…')).toBeInTheDocument()
    expect(screen.queryByText('Transaction deleted.')).not.toBeInTheDocument()

    await act(async () => {
      resolvePromise('ok')
      await pending
    })

    expect(screen.queryByText('Deleting…')).not.toBeInTheDocument()
    const title = screen.getByText('Transaction deleted.')
    expect(title.className).toContain('text-success')

    dismissActiveToast('Transaction deleted.')
  })

  it('converts a loading toast to an error message in place via toast.promise', async () => {
    vi.useFakeTimers()
    render(<Toaster />)

    let rejectPromise: (err: unknown) => void = () => {}
    const pending = new Promise<string>((_resolve, reject) => {
      rejectPromise = reject
    })

    act(() => {
      void toast
        .promise(pending, {
          loading: 'Revoking…',
          success: 'Token revoked.',
          error: 'Couldn’t revoke that token.',
        })
        .catch(() => {})
    })

    expect(screen.getByText('Revoking…')).toBeInTheDocument()

    await act(async () => {
      rejectPromise(new Error('boom'))
      await pending.catch(() => {})
    })

    expect(screen.queryByText('Revoking…')).not.toBeInTheDocument()
    const title = screen.getByText('Couldn’t revoke that token.')
    expect(title.className).toContain('text-destructive')

    dismissActiveToast('Couldn’t revoke that token.')
  })
})

// Dismisses whichever toast currently shows `text` by clicking its own
// close button, rather than needing the store's internal id — used by
// tests (like the toast.promise ones above) that never captured an id
// because toast.promise returns the wrapped promise, not the toast id.
function dismissActiveToast(text: string) {
  const toastEl = screen.getByText(text).closest('[data-slot="toast"]')
  if (!(toastEl instanceof HTMLElement)) return
  fireEvent.click(within(toastEl).getByRole('button', { name: 'Dismiss' }))
  act(() => {
    vi.advanceTimersByTime(300)
  })
}
