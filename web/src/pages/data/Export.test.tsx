// Component tests for the Export subpage (issue #214): the two download
// actions (full JSON backup, filtered CSV) call lib/api's own download
// helpers with the right arguments and report success/failure via toast.
// The helpers themselves (Blob/anchor-click mechanics) are exercised in
// lib/api.test.ts; this file only covers the screen's own wiring.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    listAccounts: vi.fn(),
    listCategories: vi.fn(),
    downloadExportJSON: vi.fn(),
    downloadExportCSV: vi.fn(),
  }
})

vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    promise: vi.fn(),
  },
}))

import {
  ApiError,
  downloadExportCSV,
  downloadExportJSON,
  listAccounts,
  listCategories,
  type Account,
} from '@/lib/api'
import { toast } from 'sonner'
import { ExportPage } from './Export'

const mockedListAccounts = vi.mocked(listAccounts)
const mockedListCategories = vi.mocked(listCategories)
const mockedDownloadJSON = vi.mocked(downloadExportJSON)
const mockedDownloadCSV = vi.mocked(downloadExportCSV)
const mockedToastSuccess = vi.mocked(toast.success)
const mockedToastError = vi.mocked(toast.error)

const checking: Account = {
  id: 'acc_1',
  name: 'Checking',
  type: 'bank',
  currency: 'USD',
  opening_balance: '0.00',
  sort_order: 0,
  archived: false,
}

describe('ExportPage', () => {
  beforeEach(() => {
    mockedListAccounts.mockReset().mockResolvedValue([checking])
    mockedListCategories.mockReset().mockResolvedValue([])
    mockedDownloadJSON.mockReset().mockResolvedValue(undefined)
    mockedDownloadCSV.mockReset().mockResolvedValue(undefined)
    mockedToastSuccess.mockReset()
    mockedToastError.mockReset()
  })

  it('downloads the full JSON backup unfiltered', async () => {
    render(<ExportPage />)
    fireEvent.click(
      await screen.findByRole('button', { name: /Download backup/ }),
    )

    await waitFor(() => expect(mockedDownloadJSON).toHaveBeenCalledWith())
    await waitFor(() =>
      expect(mockedToastSuccess).toHaveBeenCalledWith('Backup downloaded.'),
    )
  })

  it('reports a toast when the JSON backup download fails', async () => {
    mockedDownloadJSON.mockRejectedValue(
      new ApiError('internal', 'Couldn’t reach the server. Try again.'),
    )
    render(<ExportPage />)
    fireEvent.click(
      await screen.findByRole('button', { name: /Download backup/ }),
    )

    await waitFor(() =>
      expect(mockedToastError).toHaveBeenCalledWith(
        'Couldn’t reach the server. Try again.',
      ),
    )
  })

  it('downloads a CSV export scoped to the chosen account', async () => {
    const user = userEvent.setup()
    render(<ExportPage />)
    await screen.findByRole('button', { name: /Download CSV/ })

    await user.click(screen.getByRole('combobox', { name: 'Account' }))
    await user.click(await screen.findByRole('option', { name: 'Checking' }))
    fireEvent.click(screen.getByRole('button', { name: /Download CSV/ }))

    await waitFor(() =>
      expect(mockedDownloadCSV).toHaveBeenCalledWith({
        account: 'acc_1',
        category: undefined,
        type: undefined,
        from: undefined,
        to: undefined,
      }),
    )
  })

  it('downloads an unfiltered CSV export by default', async () => {
    render(<ExportPage />)
    fireEvent.click(await screen.findByRole('button', { name: /Download CSV/ }))

    await waitFor(() =>
      expect(mockedDownloadCSV).toHaveBeenCalledWith({
        account: undefined,
        category: undefined,
        type: undefined,
        from: undefined,
        to: undefined,
      }),
    )
  })
})
