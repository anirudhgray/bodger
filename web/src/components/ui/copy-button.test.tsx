import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { CopyButton } from './copy-button'

describe('CopyButton', () => {
  it('copies the value to the clipboard, then reverts after a short confirmation', async () => {
    render(<CopyButton value="bdg_secret" label="Copy token" />)

    fireEvent.click(screen.getByRole('button', { name: 'Copy token' }))

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('bdg_secret')
    expect(
      await screen.findByRole('button', { name: 'Copied' }),
    ).toBeInTheDocument()

    // The real 1.5s reset delay, not mocked away — this is one of the few
    // places in the suite a short real wait is simpler and just as
    // reliable as juggling fake timers alongside testing-library's own
    // polling internals.
    expect(
      await screen.findByRole(
        'button',
        { name: 'Copy token' },
        { timeout: 2000 },
      ),
    ).toBeInTheDocument()
  })

  it('falls back gracefully when the clipboard write is rejected', async () => {
    vi.mocked(navigator.clipboard.writeText).mockRejectedValueOnce(
      new Error('denied'),
    )

    render(<CopyButton value="bdg_secret" label="Copy token" />)
    const button = screen.getByRole('button', { name: 'Copy token' })
    fireEvent.click(button)

    // Give the rejected promise a tick to settle, then confirm the button
    // never flips to its "Copied" state and nothing throws.
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(button).toHaveAccessibleName('Copy token')
  })
})
