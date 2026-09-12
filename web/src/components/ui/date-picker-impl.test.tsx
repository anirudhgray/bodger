// Component tests for DatePicker (issue #104): the Radix
// Popover+Calendar combo it wraps needs real pointer-event sequences
// (pointerdown, focus timing) that plain fireEvent doesn't reproduce —
// see setup.ts's hasPointerCapture/scrollIntoView polyfills, added
// alongside this component for exactly this reason. Real interaction is
// exercised via @testing-library/user-event throughout, not fireEvent.
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { DatePicker } from './date-picker-impl'

describe('DatePicker', () => {
  it('controlled: opens the calendar, picks a day, and reports it via onChange', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(
      <DatePicker
        id="d"
        aria-label="Date"
        value="2026-03-15"
        onChange={onChange}
      />,
    )

    expect(screen.getByRole('button', { name: /Date/ })).toHaveTextContent(
      'Mar 15, 2026',
    )

    await user.click(screen.getByRole('button', { name: /Date/ }))
    // react-day-picker's day button sets aria-label to the full spoken
    // date ("Friday, March 20th, 2026"), which is its accessible name —
    // the visible "20" text alone isn't queryable by role.
    await screen.findByRole('button', { name: /March 20th, 2026/ })
    // Retries the click until the popover closes — see
    // TransactionDialog.test.tsx's "picks a date via the Radix date
    // picker" test (issue #164) for why a single click here can flake.
    await waitFor(async () => {
      await user.click(screen.getByRole('button', { name: /March 20th, 2026/ }))
      expect(
        screen.queryByRole('button', { name: /March 20th, 2026/ }),
      ).not.toBeInTheDocument()
    })

    expect(onChange).toHaveBeenCalledWith('2026-03-20')
    // Controlled: the trigger's own display text doesn't change until the
    // caller feeds the new value back in as `value` — confirmed by a
    // second render with the onChange result.
  })

  it('controlled: reflects a value update fed back in by the caller', async () => {
    const { rerender } = render(
      <DatePicker
        id="d"
        aria-label="Date"
        value="2026-03-15"
        onChange={() => {}}
      />,
    )
    await screen.findByRole('button', { name: /Date/ })
    rerender(
      <DatePicker
        id="d"
        aria-label="Date"
        value="2026-03-20"
        onChange={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: /Date/ })).toHaveTextContent(
      'Mar 20, 2026',
    )
  })

  it('uncontrolled: mirrors the picked value into a hidden input under the given name', async () => {
    const user = userEvent.setup()
    const { container } = render(
      <DatePicker
        id="d"
        name="from"
        aria-label="From"
        defaultValue="2026-03-15"
      />,
    )

    await screen.findByRole('button', { name: /From/ })
    const hidden = container.querySelector('input[type="hidden"][name="from"]')
    expect(hidden).toHaveValue('2026-03-15')

    await user.click(screen.getByRole('button', { name: /From/ }))
    await screen.findByRole('button', { name: /March 20th, 2026/ })
    // See the controlled test above (issue #164) for why this retries.
    await waitFor(async () => {
      await user.click(screen.getByRole('button', { name: /March 20th, 2026/ }))
      expect(
        screen.queryByRole('button', { name: /March 20th, 2026/ }),
      ).not.toBeInTheDocument()
    })

    expect(hidden).toHaveValue('2026-03-20')
    expect(screen.getByRole('button', { name: /From/ })).toHaveTextContent(
      'Mar 20, 2026',
    )
  })

  it('uncontrolled with no defaultValue: hidden input starts empty, trigger shows the placeholder', async () => {
    const { container } = render(
      <DatePicker id="d" name="to" placeholder="Pick a date" />,
    )
    await screen.findByRole('button', { name: 'Pick a date' })
    expect(
      container.querySelector('input[type="hidden"][name="to"]'),
    ).toHaveValue('')
  })
})
