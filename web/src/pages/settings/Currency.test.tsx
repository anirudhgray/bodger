// Component tests for the Currency settings subpage (issue #140).
// web/src/lib/settings is mocked here so these exercise only the page's
// own logic against a controlled fake API, the same pattern
// Password.test.tsx uses.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/settings', () => ({
  getReportingCurrency: vi.fn(),
  setReportingCurrency: vi.fn(),
}))

import { ApiError } from '@/lib/api'
import { getReportingCurrency, setReportingCurrency } from '@/lib/settings'
import { CurrencySettings } from './Currency'

const mockedGet = vi.mocked(getReportingCurrency)
const mockedSet = vi.mocked(setReportingCurrency)

describe('CurrencySettings', () => {
  beforeEach(() => {
    mockedGet.mockReset()
    mockedSet.mockReset()
  })

  it('shows the configured reporting currency once loaded', async () => {
    mockedGet.mockResolvedValue({ currency: 'EUR', is_set: true })
    render(<CurrencySettings />)

    expect(await screen.findByDisplayValue('EUR')).toBeInTheDocument()
    expect(
      screen.queryByText(/falls back to this instance's default/),
    ).not.toBeInTheDocument()
  })

  it('flags an unset reporting currency without inventing an instance default', async () => {
    mockedGet.mockResolvedValue({ currency: '', is_set: false })
    render(<CurrencySettings />)

    expect(
      await screen.findByText(/falls back to this instance's default/),
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Reporting currency')).toHaveValue('')
  })

  it('saves a new reporting currency', async () => {
    mockedGet.mockResolvedValue({ currency: '', is_set: false })
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

  it('shows the server error inline on a failed save', async () => {
    mockedGet.mockResolvedValue({ currency: 'USD', is_set: true })
    mockedSet.mockRejectedValue(
      new ApiError('invalid_input', '"XYZ" is not a known currency.'),
    )
    render(<CurrencySettings />)

    await screen.findByDisplayValue('USD')
    fireEvent.change(screen.getByLabelText('Reporting currency'), {
      target: { value: 'xyz' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      '"XYZ" is not a known currency.',
    )
  })
})
