// Component tests for the Currency settings subpage (issue #140).
// web/src/lib/settings (setReportingCurrency) and web/src/lib/api
// (getReportingCurrency lives there — issue #183) are mocked here so
// these exercise only the page's own logic against a controlled fake
// API, the same pattern Password.test.tsx uses.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/settings', () => ({
  setReportingCurrency: vi.fn(),
}))

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    getReportingCurrency: vi.fn(),
  }
})

// Saving the reporting currency is a user-triggered action, so its
// failure now reports via a toast rather than inline
// (docs/design-system.md's "Toasts vs. inline messages") — mocked here
// so tests can assert on what was shown without a real <Toaster/>
// mounted.
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    promise: vi.fn(),
  },
}))

import { ApiError, getReportingCurrency } from '@/lib/api'
import { setReportingCurrency } from '@/lib/settings'
import { CurrencySettings } from './Currency'
import { toast } from 'sonner'

const mockedGet = vi.mocked(getReportingCurrency)
const mockedSet = vi.mocked(setReportingCurrency)
const mockedToastPromise = vi.mocked(toast.promise)

describe('CurrencySettings', () => {
  beforeEach(() => {
    mockedGet.mockReset()
    mockedSet.mockReset()
    mockedToastPromise.mockReset()
  })

  it('shows the configured reporting currency once loaded', async () => {
    mockedGet.mockResolvedValue({
      currency: 'EUR',
      is_set: true,
      effectiveCurrency: 'EUR',
    })
    render(<CurrencySettings />)

    expect(await screen.findByDisplayValue('EUR')).toBeInTheDocument()
    expect(
      screen.queryByText(/falls back to this instance's default/),
    ).not.toBeInTheDocument()
  })

  it('flags an unset reporting currency without inventing an instance default', async () => {
    mockedGet.mockResolvedValue({
      currency: '',
      is_set: false,
      effectiveCurrency: 'USD',
    })
    render(<CurrencySettings />)

    expect(
      await screen.findByText(/falls back to this instance's default/),
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Reporting currency')).toHaveValue('')
  })

  it('saves a new reporting currency', async () => {
    mockedGet.mockResolvedValue({
      currency: '',
      is_set: false,
      effectiveCurrency: 'USD',
    })
    mockedSet.mockResolvedValue(undefined)
    render(<CurrencySettings />)

    await screen.findByLabelText('Reporting currency')
    fireEvent.change(screen.getByLabelText('Reporting currency'), {
      target: { value: 'gbp' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(mockedSet).toHaveBeenCalledWith('GBP'))
    expect(await screen.findByDisplayValue('GBP')).toBeInTheDocument()
    expect(
      screen.queryByText(/falls back to this instance's default/),
    ).not.toBeInTheDocument()
  })

  it('shows a toast (not inline) on a failed save', async () => {
    mockedGet.mockResolvedValue({
      currency: 'USD',
      is_set: true,
      effectiveCurrency: 'USD',
    })
    mockedSet.mockRejectedValue(
      new ApiError('invalid_input', '"XYZ" is not a known currency.'),
    )
    render(<CurrencySettings />)

    await screen.findByDisplayValue('USD')
    fireEvent.change(screen.getByLabelText('Reporting currency'), {
      target: { value: 'xyz' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    // Setting the reporting currency is a user-triggered action
    // (docs/design-system.md's "Toasts vs. inline messages"), so its
    // failure is reported via toast.promise's `error` option, not
    // inline.
    await waitFor(() => expect(mockedToastPromise).toHaveBeenCalled())
    const [, options] = mockedToastPromise.mock.calls[0]
    const toastError = options?.error as (err: unknown) => string
    expect(
      toastError(
        new ApiError('invalid_input', '"XYZ" is not a known currency.'),
      ),
    ).toBe('"XYZ" is not a known currency.')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
