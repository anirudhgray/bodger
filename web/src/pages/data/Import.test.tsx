// Component tests for the Import subpage (issue #214): the upload → map
// columns → review → commit wizard, and the import history list's own
// continue-review/roll-back actions. lib/api is mocked throughout — these
// exercise this screen's own sequencing against a controlled fake API,
// not the real REST surface (that's internal/surface/http/imports_test.go's
// job).
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    listAccounts: vi.fn(),
    listImportBatches: vi.fn(),
    getImportBatch: vi.fn(),
    listImportRecords: vi.fn(),
    uploadImport: vi.fn(),
    resolveImportRecord: vi.fn(),
    commitImportBatch: vi.fn(),
    rollbackImportBatch: vi.fn(),
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
  commitImportBatch,
  getImportBatch,
  listAccounts,
  listImportBatches,
  listImportRecords,
  resolveImportRecord,
  rollbackImportBatch,
  uploadImport,
  type Account,
  type ImportBatch,
  type ImportRecord,
} from '@/lib/api'
import { toast } from 'sonner'
import { ImportPage } from './Import'

const mockedListAccounts = vi.mocked(listAccounts)
const mockedListImportBatches = vi.mocked(listImportBatches)
const mockedGetImportBatch = vi.mocked(getImportBatch)
const mockedListImportRecords = vi.mocked(listImportRecords)
const mockedUploadImport = vi.mocked(uploadImport)
const mockedResolveImportRecord = vi.mocked(resolveImportRecord)
const mockedCommitImportBatch = vi.mocked(commitImportBatch)
const mockedRollbackImportBatch = vi.mocked(rollbackImportBatch)
const mockedToastError = vi.mocked(toast.error)
const mockedToastPromise = vi.mocked(toast.promise)

const checking: Account = {
  id: 'acc_1',
  name: 'Checking',
  type: 'bank',
  currency: 'USD',
  opening_balance: '0.00',
  sort_order: 0,
  archived: false,
}

function makeBatch(overrides: Partial<ImportBatch> = {}): ImportBatch {
  return {
    id: 'batch_1',
    source_format: 'csv',
    filename: 'statement.csv',
    file_hash: 'hash123',
    target_account_id: 'acc_1',
    status: 'staged',
    ...overrides,
  }
}

function makeRecord(overrides: Partial<ImportRecord> = {}): ImportRecord {
  return {
    id: 'rec_1',
    import_id: 'batch_1',
    booked_date: '2026-01-05',
    description: 'Coffee',
    amount: '-4.50',
    currency: 'USD',
    status: 'pending',
    sort_order: 0,
    ...overrides,
  }
}

function csvFile(): File {
  return new File(
    ['Date,Description,Amount\n2026-01-05,Coffee,-4.50\n'],
    'statement.csv',
    { type: 'text/csv' },
  )
}

describe('ImportPage', () => {
  beforeEach(() => {
    mockedListAccounts.mockReset().mockResolvedValue([checking])
    mockedListImportBatches.mockReset().mockResolvedValue([])
    mockedGetImportBatch.mockReset()
    mockedListImportRecords.mockReset()
    mockedUploadImport.mockReset()
    mockedResolveImportRecord.mockReset()
    mockedCommitImportBatch.mockReset()
    mockedRollbackImportBatch.mockReset()
    mockedToastError.mockReset()
    mockedToastPromise.mockReset()
  })

  it('shows import history with a status per batch', async () => {
    mockedListImportBatches.mockResolvedValue([
      makeBatch({ id: 'batch_1', filename: 'jan.csv', status: 'staged' }),
      makeBatch({ id: 'batch_2', filename: 'feb.csv', status: 'committed' }),
      makeBatch({ id: 'batch_3', filename: 'mar.csv', status: 'rolled_back' }),
    ])
    render(<ImportPage />)

    expect(await screen.findByText('jan.csv')).toBeInTheDocument()
    expect(screen.getByText('feb.csv')).toBeInTheDocument()
    expect(screen.getByText('mar.csv')).toBeInTheDocument()
    expect(screen.getByText('Staged')).toBeInTheDocument()
    expect(screen.getByText('Committed')).toBeInTheDocument()
    expect(screen.getByText('Rolled back')).toBeInTheDocument()

    // Only a staged/reviewed import can be continued, only a committed one
    // rolled back.
    expect(
      screen.getByRole('button', { name: 'Continue review' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Roll back' }),
    ).toBeInTheDocument()
  })

  it('walks upload → map columns → review → commit end to end', async () => {
    const user = userEvent.setup()
    const stagedBatch = makeBatch()
    const suspectRecord = makeRecord({
      duplicate_match: {
        tier: 'suspected_duplicate',
        matched_transaction_id: 'tx_existing',
        resolution: 'pending',
      },
    })
    mockedUploadImport.mockResolvedValue({
      batch: stagedBatch,
      records: [suspectRecord],
    })
    mockedResolveImportRecord.mockResolvedValue({
      ...suspectRecord,
      status: 'ready',
      duplicate_match: {
        tier: 'suspected_duplicate',
        matched_transaction_id: 'tx_existing',
        resolution: 'not_duplicate',
      },
    })
    mockedCommitImportBatch.mockResolvedValue({
      batch: { ...stagedBatch, status: 'committed' },
      transactions: [
        {
          id: 'tx_new',
          amount: '4.50',
          currency: 'USD',
          date: '2026-01-05',
          description: 'Coffee',
          type: 'outflow',
          account_id: 'acc_1',
        },
      ],
    })

    render(<ImportPage />)
    await user.click(await screen.findByRole('button', { name: 'New import' }))

    // Upload step: pick the account and a file.
    await user.click(screen.getByRole('combobox', { name: 'Account' }))
    await user.click(await screen.findByRole('option', { name: 'Checking' }))
    await user.upload(screen.getByLabelText('File'), csvFile())
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    // Mapping step: the header row is enough to auto-guess every
    // required column.
    expect(
      await screen.findByRole('combobox', { name: 'Date' }),
    ).toHaveTextContent('Date')
    expect(
      screen.getByRole('combobox', { name: 'Description' }),
    ).toHaveTextContent('Description')
    expect(screen.getByRole('combobox', { name: 'Amount' })).toHaveTextContent(
      'Amount',
    )
    await user.click(screen.getByRole('button', { name: 'Stage for review' }))

    await waitFor(() =>
      expect(mockedUploadImport).toHaveBeenCalledWith({
        account: 'acc_1',
        filename: 'statement.csv',
        fileContent: 'Date,Description,Amount\n2026-01-05,Coffee,-4.50\n',
        mapping: {
          dateColumn: 'Date',
          descriptionColumn: 'Description',
          amountColumn: 'Amount',
          postedDateColumn: undefined,
          currencyColumn: undefined,
          externalIDColumn: undefined,
          categoryColumn: undefined,
        },
      }),
    )

    // Review step: a suspected duplicate blocks commit until resolved.
    expect(await screen.findByText('Coffee')).toBeInTheDocument()
    expect(screen.getByText('Suspected duplicate')).toBeInTheDocument()
    const commitButton = screen.getByRole('button', { name: 'Commit import' })
    expect(commitButton).toBeDisabled()

    await user.click(screen.getByRole('button', { name: 'Not a duplicate' }))
    await waitFor(() =>
      expect(mockedResolveImportRecord).toHaveBeenCalledWith(
        'rec_1',
        'not_duplicate',
      ),
    )
    expect(await screen.findByText('Not a duplicate')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Commit import' })).toBeEnabled()

    await user.click(screen.getByRole('button', { name: 'Commit import' }))
    await waitFor(() =>
      expect(mockedCommitImportBatch).toHaveBeenCalledWith('batch_1'),
    )
    // Committing closes the wizard and refreshes history.
    await waitFor(() =>
      expect(
        screen.queryByRole('button', { name: 'Commit import' }),
      ).not.toBeInTheDocument(),
    )
  })

  it('continues review on a staged import from history', async () => {
    const user = userEvent.setup()
    mockedListImportBatches.mockResolvedValue([makeBatch({ status: 'staged' })])
    mockedGetImportBatch.mockResolvedValue(makeBatch({ status: 'staged' }))
    mockedListImportRecords.mockResolvedValue([makeRecord({ status: 'ready' })])

    render(<ImportPage />)
    await user.click(
      await screen.findByRole('button', { name: 'Continue review' }),
    )

    await waitFor(() =>
      expect(mockedGetImportBatch).toHaveBeenCalledWith('batch_1'),
    )
    expect(mockedListImportRecords).toHaveBeenCalledWith('batch_1')
    expect(await screen.findByText('Coffee')).toBeInTheDocument()
  })

  it('rolls back a committed import after confirming', async () => {
    const user = userEvent.setup()
    mockedListImportBatches
      .mockResolvedValueOnce([makeBatch({ status: 'committed' })])
      .mockResolvedValueOnce([makeBatch({ status: 'rolled_back' })])
    mockedRollbackImportBatch.mockResolvedValue({
      batch: makeBatch({ status: 'rolled_back' }),
      transaction_ids: ['tx_new'],
    })

    render(<ImportPage />)
    await user.click(await screen.findByRole('button', { name: 'Roll back' }))
    const dialog = await screen.findByRole('alertdialog')
    await user.click(within(dialog).getByRole('button', { name: 'Roll back' }))

    await waitFor(() =>
      expect(mockedRollbackImportBatch).toHaveBeenCalledWith('batch_1'),
    )
    expect(mockedToastPromise).toHaveBeenCalled()
  })
})
