import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    getFxRate: vi.fn(),
    fetchFxRates: vi.fn(),
  }
})

import { ApiError, fetchFxRates, getFxRate, type FxRate } from '@/lib/api'
import { useFxConversionHint } from './use-fx-conversion-hint'

const mockedGetFxRate = vi.mocked(getFxRate)
const mockedFetchFxRates = vi.mocked(fetchFxRates)

const rate: FxRate = {
  from: 'INR',
  to: 'USD',
  rate: '0.012',
  rate_date: '2026-09-05',
  rate_source: 'frankfurter',
  stale: false,
  policy: 'current',
  amount: '800',
  converted: '9.60',
}

beforeEach(() => {
  vi.useFakeTimers()
  mockedGetFxRate.mockReset()
  mockedFetchFxRates.mockReset()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useFxConversionHint', () => {
  it('stays idle when the two currencies are the same', async () => {
    const { result } = renderHook(() =>
      useFxConversionHint({ from: 'USD', to: 'USD', amount: '10', date: '' }),
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })
    expect(result.current.state).toEqual({ status: 'idle' })
    expect(mockedGetFxRate).not.toHaveBeenCalled()
  })

  it('stays idle when the amount is empty', async () => {
    const { result } = renderHook(() =>
      useFxConversionHint({ from: 'INR', to: 'USD', amount: '  ', date: '' }),
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })
    expect(result.current.state).toEqual({ status: 'idle' })
    expect(mockedGetFxRate).not.toHaveBeenCalled()
  })

  it('debounces the lookup and resolves to "ready" with policy=current when no date is entered', async () => {
    mockedGetFxRate.mockResolvedValue(rate)
    const { result } = renderHook(() =>
      useFxConversionHint({
        from: 'INR',
        to: 'USD',
        amount: '800',
        date: '',
      }),
    )

    // Not yet called before the debounce window elapses.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100)
    })
    expect(mockedGetFxRate).not.toHaveBeenCalled()

    await act(async () => {
      await vi.advanceTimersByTimeAsync(400)
    })
    expect(result.current.state).toEqual({ status: 'ready', rate })
    expect(mockedGetFxRate).toHaveBeenCalledWith({
      from: 'INR',
      to: 'USD',
      amount: '800',
      policy: 'current',
    })
  })

  it('looks up policy=transaction_date at the entered date once backdated', async () => {
    mockedGetFxRate.mockResolvedValue(rate)
    renderHook(() =>
      useFxConversionHint({
        from: 'INR',
        to: 'USD',
        amount: '800',
        date: '2026-08-14',
      }),
    )

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })
    expect(mockedGetFxRate).toHaveBeenCalledWith({
      from: 'INR',
      to: 'USD',
      amount: '800',
      policy: 'transaction_date',
      transactionDate: '2026-08-14',
    })
  })

  it('surfaces a 404 as "unavailable" rather than an error', async () => {
    mockedGetFxRate.mockRejectedValue(
      new ApiError('not_found', 'No INR/USD rate available.'),
    )
    const { result } = renderHook(() =>
      useFxConversionHint({ from: 'INR', to: 'USD', amount: '800', date: '' }),
    )

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })
    expect(result.current.state).toEqual({ status: 'unavailable' })
  })

  it('surfaces any other failure as an error message', async () => {
    mockedGetFxRate.mockRejectedValue(
      new ApiError('internal', 'Something went wrong.'),
    )
    const { result } = renderHook(() =>
      useFxConversionHint({ from: 'INR', to: 'USD', amount: '800', date: '' }),
    )

    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })
    expect(result.current.state).toEqual({
      status: 'error',
      message: 'Something went wrong.',
    })
  })

  it('ignores a stale response that resolves after a newer request already has', async () => {
    let resolveFirst!: (rate: FxRate) => void
    mockedGetFxRate.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveFirst = resolve
        }),
    )
    const { result, rerender } = renderHook(
      (props: { amount: string }) =>
        useFxConversionHint({
          from: 'INR',
          to: 'USD',
          amount: props.amount,
          date: '',
        }),
      { initialProps: { amount: '8' } },
    )

    await act(async () => {
      await vi.advanceTimersByTimeAsync(400)
    })
    expect(mockedGetFxRate).toHaveBeenCalledTimes(1)

    mockedGetFxRate.mockResolvedValueOnce({ ...rate, converted: '9.60' })
    rerender({ amount: '800' })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(400)
    })
    expect(result.current.state).toEqual({
      status: 'ready',
      rate: { ...rate, converted: '9.60' },
    })

    // The first ("8") request finally resolves after the second ("800")
    // already has — it must not clobber the newer result.
    await act(async () => {
      resolveFirst({ ...rate, converted: '0.096' })
      await Promise.resolve()
    })
    expect(result.current.state).toEqual({
      status: 'ready',
      rate: { ...rate, converted: '9.60' },
    })
  })

  it('refresh() fetches exactly the "from" base currency for the entered date, then reloads', async () => {
    mockedGetFxRate.mockResolvedValue(rate)
    mockedFetchFxRates.mockResolvedValue({
      reporting_currency: 'USD',
      fetched: [
        {
          pair: 'INR/USD',
          rate: '0.012',
          date: '2026-08-14',
          source: 'frankfurter',
        },
      ],
    })
    const { result } = renderHook(() =>
      useFxConversionHint({
        from: 'INR',
        to: 'USD',
        amount: '800',
        date: '2026-08-14',
      }),
    )
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500)
    })
    expect(result.current.canRefresh).toBe(true)

    await act(async () => {
      result.current.refresh()
      await vi.advanceTimersByTimeAsync(0)
    })

    expect(mockedFetchFxRates).toHaveBeenCalledWith(['INR'], '2026-08-14')
    expect(result.current.refreshing).toBe(false)
    // refresh() re-runs load() directly (not through the debounce), so
    // the rate lookup ran a second time.
    expect(mockedGetFxRate).toHaveBeenCalledTimes(2)
  })
})
