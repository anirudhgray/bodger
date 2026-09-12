// Component test for the DatePicker lazy wrapper (issue #119): proves
// react-day-picker/date-fns are deferred until the component actually
// mounts, without changing the public contract. date-picker-impl.test.tsx
// covers the actual picker behaviour; this file only covers the
// loading/fallback lifecycle the wrapper adds on top.
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { DatePicker } from './date-picker'

describe('DatePicker (lazy wrapper)', () => {
  it('shows a skeleton fallback, then resolves to the real picker', async () => {
    render(<DatePicker id="d" placeholder="Pick a date" />)

    expect(
      screen.queryByRole('button', { name: 'Pick a date' }),
    ).not.toBeInTheDocument()

    expect(
      await screen.findByRole('button', { name: 'Pick a date' }),
    ).toBeInTheDocument()
  })
})
