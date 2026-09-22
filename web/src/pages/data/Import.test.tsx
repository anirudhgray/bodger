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
    listCategories: vi.fn(),
    listImportBatches: vi.fn(),
    getImportBatch: vi.fn(),
    getImportSuggestions: vi.fn(),
    listImportRecords: vi.fn(),
    uploadImport: vi.fn(),
    resolveImportRecord: vi.fn(),
    resolveImportRecordOccurrenceMatch: vi.fn(),
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
  getImportSuggestions,
  listAccounts,
  listCategories,
  listImportBatches,
  listImportRecords,
  resolveImportRecord,
  resolveImportRecordOccurrenceMatch,
  rollbackImportBatch,
  uploadImport,
  type Account,
  type Category,
  type ImportBatch,
  type ImportRecord,
  type ImportSuggestions,
} from '@/lib/api'
import { toast } from 'sonner'
import { ImportPage } from './Import'

const mockedListAccounts = vi.mocked(listAccounts)
const mockedListCategories = vi.mocked(listCategories)
const mockedListImportBatches = vi.mocked(listImportBatches)
const mockedGetImportBatch = vi.mocked(getImportBatch)
const mockedGetImportSuggestions = vi.mocked(getImportSuggestions)
const mockedListImportRecords = vi.mocked(listImportRecords)
const mockedUploadImport = vi.mocked(uploadImport)
const mockedResolveImportRecord = vi.mocked(resolveImportRecord)
const mockedResolveImportRecordOccurrenceMatch = vi.mocked(
  resolveImportRecordOccurrenceMatch,
)
const mockedCommitImportBatch = vi.mocked(commitImportBatch)
const mockedRollbackImportBatch = vi.mocked(rollbackImportBatch)
const mockedToastError = vi.mocked(toast.error)
const mockedToastPromise = vi.mocked(toast.promise)
const mockedToastSuccess = vi.mocked(toast.success)

const checking: Account = {
  id: 'acc_1',
  name: 'Checking',
  type: 'bank',
  currency: 'USD',
  opening_balance: '0.00',
  sort_order: 0,
  archived: false,
}

const groceries: Category = {
  id: 'cat_1',
  name: 'Groceries',
  type: 'expense',
  sort_order: 0,
  archived: false,
}

function notConfiguredSuggestions(): ImportSuggestions {
  return {
    configured: false,
    rows_failed: 0,
    rows_suggested: 0,
    suggestions: [],
  }
}

function failedSuggestions(
  overrides: Partial<ImportSuggestions> = {},
): ImportSuggestions {
  return {
    configured: true,
    rows_failed: 1,
    rows_suggested: 1,
    failure_reason: 'credential_rejected',
    suggestions: [],
    ...overrides,
  }
}

function presentSuggestions(
  overrides: Partial<ImportSuggestions> = {},
): ImportSuggestions {
  return {
    configured: true,
    rows_failed: 0,
    rows_suggested: 1,
    suggestions: [
      {
        record_id: 'rec_1',
        source: 'typesafe.ai',
        category: { category_id: 'cat_1', confidence: 0.87 },
      },
    ],
    ...overrides,
  }
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
    mockedListCategories.mockReset().mockResolvedValue([groceries])
    mockedListImportBatches.mockReset().mockResolvedValue([])
    mockedGetImportBatch.mockReset()
    mockedGetImportSuggestions
      .mockReset()
      .mockResolvedValue(notConfiguredSuggestions())
    mockedListImportRecords.mockReset()
    mockedUploadImport.mockReset()
    mockedResolveImportRecord.mockReset()
    mockedResolveImportRecordOccurrenceMatch.mockReset()
    mockedCommitImportBatch.mockReset()
    mockedRollbackImportBatch.mockReset()
    mockedToastError.mockReset()
    mockedToastPromise.mockReset()
    mockedToastSuccess.mockReset()
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

  // Issue #307's own three-state acceptance criterion (present / not
  // configured / failed) plus the #309/#312 web-UI gap this PR closes
  // alongside it (issue #307's user-confirmed scope). getImportSuggestions
  // is fetched once when review opens (see "continues review on a staged
  // import from history" above for the same Continue-review entry point),
  // never on upload/mapping.
  describe('AI-assisted suggestions', () => {
    async function openReview(
      records: ImportRecord[],
      batch: ImportBatch = makeBatch({ status: 'staged' }),
    ) {
      const user = userEvent.setup()
      mockedListImportBatches.mockResolvedValue([batch])
      mockedGetImportBatch.mockResolvedValue(batch)
      mockedListImportRecords.mockResolvedValue(records)

      render(<ImportPage />)
      await user.click(
        await screen.findByRole('button', { name: 'Continue review' }),
      )
      await waitFor(() =>
        expect(mockedGetImportSuggestions).toHaveBeenCalledWith(batch.id),
      )
      return user
    }

    it('shows a loading placeholder in the Suggested column while the request is in flight, instead of popping content in', async () => {
      // Before suggestionsLoading existed, the Suggested column rendered
      // "—" for every row until getImportSuggestions resolved, then
      // suddenly swapped in a taller suggestion card — a real,
      // user-reported layout shift with nothing to indicate a request was
      // even in flight.
      let resolveSuggestions!: (value: ImportSuggestions) => void
      mockedGetImportSuggestions.mockReturnValue(
        new Promise((resolve) => {
          resolveSuggestions = resolve
        }),
      )
      const batch = makeBatch({ status: 'staged' })
      mockedListImportBatches.mockResolvedValue([batch])
      mockedGetImportBatch.mockResolvedValue(batch)
      mockedListImportRecords.mockResolvedValue([
        makeRecord({ status: 'ready' }),
      ])

      const user = userEvent.setup()
      const { container } = render(<ImportPage />)
      await user.click(
        await screen.findByRole('button', { name: 'Continue review' }),
      )
      await waitFor(() =>
        expect(mockedGetImportSuggestions).toHaveBeenCalledWith(batch.id),
      )

      expect(await screen.findByText('Coffee')).toBeInTheDocument()
      expect(container.querySelector('[data-slot="skeleton"]')).toBeTruthy()
      // Not yet claiming anything about configuration — the request
      // hasn't resolved, so no notice line should exist either.
      expect(
        screen.queryByText(
          'AI-assisted suggestions aren’t set up on this instance.',
        ),
      ).not.toBeInTheDocument()

      resolveSuggestions(notConfiguredSuggestions())
      await waitFor(() =>
        expect(
          container.querySelector('[data-slot="skeleton"]'),
        ).not.toBeInTheDocument(),
      )
      expect(
        screen.getByText(
          'AI-assisted suggestions aren’t set up on this instance.',
        ),
      ).toBeInTheDocument()
    })

    it('shows a quiet, non-blocking line when no typesafe.ai key is configured', async () => {
      mockedGetImportSuggestions.mockResolvedValue(notConfiguredSuggestions())
      await openReview([makeRecord({ status: 'ready' })])

      expect(await screen.findByText('Coffee')).toBeInTheDocument()
      expect(
        screen.getByText(
          'AI-assisted suggestions aren’t set up on this instance.',
        ),
      ).toBeInTheDocument()
      // The screen stays exactly as usable as without the feature — no
      // suggestion cell content, no blocked commit.
      expect(
        screen.getByRole('button', { name: 'Commit import' }),
      ).toBeEnabled()
    })

    it('shows a plain-English failure reason, never the raw code, and keeps every row reviewable', async () => {
      mockedGetImportSuggestions.mockResolvedValue(
        failedSuggestions({ failure_reason: 'credential_rejected' }),
      )
      await openReview([makeRecord({ status: 'ready' })])

      expect(await screen.findByText('Coffee')).toBeInTheDocument()
      expect(
        screen.getByText(/your typesafe\.ai credential was rejected/),
      ).toBeInTheDocument()
      expect(screen.queryByText('credential_rejected')).not.toBeInTheDocument()
      // Not an error page over data sitting in SQLite: the row and its
      // commit path are unaffected.
      expect(
        screen.getByRole('button', { name: 'Commit import' }),
      ).toBeEnabled()
    })

    it('shows a ranked, typesafe.ai-attributed category suggestion with no confidence decimal', async () => {
      mockedGetImportSuggestions.mockResolvedValue(presentSuggestions())
      await openReview([makeRecord({ status: 'ready' })])

      expect(await screen.findByText('Coffee')).toBeInTheDocument()
      expect(screen.getByText('Groceries')).toBeInTheDocument()
      expect(
        screen.getAllByText(/Suggested by typesafe\.ai/).length,
      ).toBeGreaterThan(0)
      expect(screen.queryByText('0.87')).not.toBeInTheDocument()
      // Advisory only — no write happened and no accept control exists for
      // a category suggestion (#305/#306: no such endpoint).
      expect(mockedResolveImportRecordOccurrenceMatch).not.toHaveBeenCalled()
    })

    it('accepts an AI-only occurrence match (no deterministic match) through the Decision column, passing its occurrence id', async () => {
      // The real-world case a manual test against a live typesafe.ai key
      // found broken: findOccurrenceMatch's own staging-time gate missed
      // this match (no occurrence_match at all, status already ready —
      // nothing held the record pending on it), but
      // SuggestForImportBatch's own looser candidate narrowing still
      // offered it. Accepting it is the *only* place this decision is
      // ever offered (see ReviewStep's needsAIOnlyOccurrenceDecision) —
      // there is no separate "Accept match" control any more.
      const suggested = makeRecord({ status: 'ready' })
      mockedGetImportSuggestions.mockResolvedValue(
        presentSuggestions({
          suggestions: [
            {
              record_id: 'rec_1',
              source: 'typesafe.ai',
              occurrence: { occurrence_id: 'occ_1', confidence: 0.91 },
            },
          ],
        }),
      )
      mockedResolveImportRecordOccurrenceMatch.mockResolvedValue({
        ...suggested,
        status: 'excluded',
        occurrence_match: {
          occurrence_id: 'occ_1',
          resolution: 'materialized',
        },
      })
      const user = await openReview([suggested])

      expect(await screen.findByText('Projected match')).toBeInTheDocument()
      // No deterministic-match flag — this row's own detection never
      // caught it, which is the whole point of this scenario.
      expect(
        screen.queryByText('Matches a pending occurrence'),
      ).not.toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: 'Materialise' }))
      await waitFor(() =>
        expect(mockedResolveImportRecordOccurrenceMatch).toHaveBeenCalledWith(
          'rec_1',
          'materialized',
          'occ_1',
        ),
      )
      expect(mockedToastSuccess).toHaveBeenCalledWith(
        'Materialised — recorded as a transaction.',
      )
      expect(
        await screen.findByText('Occurrence materialised'),
      ).toBeInTheDocument()
    })

    it('never shows an AI-only occurrence decision once the row has already committed or excluded', async () => {
      // Defence in depth, matching the backend's own terminal-status
      // guard: once a row is committed or excluded, accepting an AI
      // suggestion for it can no longer do anything safe (it would
      // materialise a second, genuinely double-counted transaction), so
      // the UI never offers the control at all rather than letting a
      // click surface a server-side refusal.
      mockedGetImportSuggestions.mockResolvedValue(
        presentSuggestions({
          suggestions: [
            {
              record_id: 'rec_1',
              source: 'typesafe.ai',
              occurrence: { occurrence_id: 'occ_1', confidence: 0.91 },
            },
          ],
        }),
      )
      await openReview([
        makeRecord({ status: 'committed', transaction_id: 'txn_1' }),
      ])

      expect(await screen.findByText('Projected match')).toBeInTheDocument()
      expect(
        screen.queryByRole('button', { name: 'Materialise' }),
      ).not.toBeInTheDocument()
    })

    it('offers only one Decision-column control when a deterministic match and an AI suggestion agree on the same row', async () => {
      // A row findOccurrenceMatch already caught (occurrence_match set,
      // pending) whose AI-suggested candidate happens to be the same
      // occurrence — the common case, since both draw from the same
      // eligibleOccurrences narrowing. This must render exactly one
      // Materialise/"Not this occurrence" pair, using the record's own
      // matched occurrence id (the AI suggestion's is ignored server-side
      // either way — see resolveImportRecordOccurrenceMatch's own doc
      // comment — so the UI passes no occurrenceId for this case).
      const matched = makeRecord({
        status: 'pending',
        occurrence_match: { occurrence_id: 'occ_1', resolution: 'pending' },
      })
      mockedGetImportSuggestions.mockResolvedValue(
        presentSuggestions({
          suggestions: [
            {
              record_id: 'rec_1',
              source: 'typesafe.ai',
              occurrence: { occurrence_id: 'occ_1', confidence: 0.91 },
            },
          ],
        }),
      )
      mockedResolveImportRecordOccurrenceMatch.mockResolvedValue({
        ...matched,
        status: 'excluded',
        occurrence_match: {
          occurrence_id: 'occ_1',
          resolution: 'materialized',
        },
      })
      const user = await openReview([matched])

      expect(await screen.findByText('Projected match')).toBeInTheDocument()
      expect(
        screen.getByText('Matches a pending occurrence'),
      ).toBeInTheDocument()
      expect(
        screen.getAllByRole('button', { name: 'Materialise' }),
      ).toHaveLength(1)

      await user.click(screen.getByRole('button', { name: 'Materialise' }))
      await waitFor(() =>
        expect(mockedResolveImportRecordOccurrenceMatch).toHaveBeenCalledWith(
          'rec_1',
          'materialized',
          undefined,
        ),
      )
    })

    it('dismisses a deterministic occurrence match independently of any duplicate decision', async () => {
      mockedGetImportSuggestions.mockResolvedValue(notConfiguredSuggestions())
      const record = makeRecord({
        status: 'pending',
        occurrence_match: { occurrence_id: 'occ_1', resolution: 'pending' },
      })
      mockedResolveImportRecordOccurrenceMatch.mockResolvedValue({
        ...record,
        status: 'ready',
        occurrence_match: { occurrence_id: 'occ_1', resolution: 'dismissed' },
      })
      const user = await openReview([record])

      expect(
        await screen.findByText('Matches a pending occurrence'),
      ).toBeInTheDocument()
      await user.click(
        screen.getByRole('button', { name: 'Not this occurrence' }),
      )
      await waitFor(() =>
        expect(mockedResolveImportRecordOccurrenceMatch).toHaveBeenCalledWith(
          'rec_1',
          'dismissed',
          undefined,
        ),
      )
      expect(await screen.findByText('Not this occurrence')).toBeInTheDocument()
      // Dismissing doesn't claim a transaction was created.
      expect(mockedToastSuccess).not.toHaveBeenCalled()
    })
  })
})
