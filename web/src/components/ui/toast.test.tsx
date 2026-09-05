// Toast doesn't need the @testing-library/user-event + jsdom polyfills
// (hasPointerCapture, scrollIntoView) issue #104 tracks for the other
// Radix-portal primitives — like Dialog and Switch, it works fine under
// plain fireEvent (confirmed in #108's issue body) since it needs neither
// pointer-capture nor scroll-into-view.
import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { dismissToast, toast } from '@/hooks/use-toast'

import { Toaster } from './toast'

describe('Toaster', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders a toast with its variant styling', () => {
    vi.useFakeTimers()
    render(<Toaster />)

    let id = ''
    act(() => {
      id = toast({ description: 'Transaction recorded.', variant: 'success' })
    })

    const description = screen.getByText('Transaction recorded.')
    expect(description).toBeInTheDocument()

    const toastEl = description.closest('[data-slot="toast"]')
    expect(toastEl).not.toBeNull()
    expect(toastEl?.className).toContain('border-success/20')

    // Clean up so this toast doesn't leak into the next test's store —
    // the module-level store (hooks/use-toast.ts) is shared across every
    // <Toaster/> instance rendered in this file.
    act(() => {
      dismissToast(id)
      vi.advanceTimersByTime(300)
    })
  })

  it('dismisses a toast when its close button is clicked', async () => {
    vi.useFakeTimers()
    render(<Toaster />)

    act(() => {
      toast({ description: 'Token revoked.' })
    })
    expect(screen.getByText('Token revoked.')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }))

    act(() => {
      vi.advanceTimersByTime(300)
    })

    expect(screen.queryByText('Token revoked.')).not.toBeInTheDocument()
  })
})
