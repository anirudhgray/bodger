// Component tests for the Restore subpage (issue #214) — the one flow in
// the web UI that wipes and replaces everything, so these focus on the
// gate itself: restoreSnapshot must never fire without both a chosen file
// and the exact confirmation phrase typed, matching what this screen's
// own dialog displays (not a plain "Yes" click). lib/api is mocked
// throughout.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    restoreSnapshot: vi.fn(),
  }
})

import { ApiError, restoreSnapshot } from '@/lib/api'
import { RestorePage } from './Restore'

const mockedRestoreSnapshot = vi.mocked(restoreSnapshot)

// The exact phrase Restore.tsx's own dialog asks a person to type — kept
// as a literal here (not imported) so this test fails if the visible
// instruction and the gate it checks against ever drift apart.
const CONFIRM_PHRASE = 'REPLACE ALL MY DATA'

function backupFile(document: unknown = { format: 'bodger.export/v1' }) {
  return new File([JSON.stringify(document)], 'backup.json', {
    type: 'application/json',
  })
}

// RestorePage calls useNavigate (for the "Go to transactions" button
// after a successful restore) — a MemoryRouter is enough to satisfy
// react-router-dom's context without pulling in the real route tree.
function renderWithRouter() {
  return render(
    <MemoryRouter>
      <RestorePage />
    </MemoryRouter>,
  )
}

describe('RestorePage', () => {
  beforeEach(() => {
    mockedRestoreSnapshot.mockReset()
  })

  it('shows the destructive warning up front', () => {
    renderWithRouter()
    expect(
      screen.getByText('This replaces everything you currently have'),
    ).toBeInTheDocument()
  })

  it('rejects a file that is not valid JSON, without offering to restore', async () => {
    const user = userEvent.setup()
    renderWithRouter()

    await user.upload(
      screen.getByLabelText('Backup file'),
      new File(['not json'], 'bad.json', { type: 'application/json' }),
    )

    expect(
      await screen.findByText(/doesn’t look like a valid backup file/),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /Replace everything/ }),
    ).not.toBeInTheDocument()
  })

  it('never calls restoreSnapshot without the exact confirmation phrase', async () => {
    const user = userEvent.setup()
    renderWithRouter()

    await user.upload(screen.getByLabelText('Backup file'), backupFile())
    await user.click(
      await screen.findByRole('button', { name: /Replace everything…/ }),
    )

    const submit = screen.getByRole('button', { name: 'Replace everything' })
    expect(submit).toBeDisabled()

    await user.type(
      screen.getByLabelText(/Type .* to confirm/),
      'replace all my data', // wrong case
    )
    expect(submit).toBeDisabled()
    expect(mockedRestoreSnapshot).not.toHaveBeenCalled()

    fireEvent.click(submit)
    expect(mockedRestoreSnapshot).not.toHaveBeenCalled()
  })

  it('restores once the exact phrase is typed, then shows a summary', async () => {
    const user = userEvent.setup()
    const document = { format: 'bodger.export/v1', accounts: [] }
    mockedRestoreSnapshot.mockResolvedValue({
      accounts: 3,
      categories: 5,
      transactions: 42,
      budgets: 2,
    })
    renderWithRouter()

    await user.upload(
      screen.getByLabelText('Backup file'),
      backupFile(document),
    )
    await user.click(
      await screen.findByRole('button', { name: /Replace everything…/ }),
    )
    await user.type(screen.getByLabelText(/Type .* to confirm/), CONFIRM_PHRASE)
    const submit = screen.getByRole('button', { name: 'Replace everything' })
    expect(submit).toBeEnabled()
    fireEvent.click(submit)

    await waitFor(() =>
      expect(mockedRestoreSnapshot).toHaveBeenCalledWith(document, true),
    )
    expect(await screen.findByText('Restore complete')).toBeInTheDocument()
    expect(
      screen.getByText(
        /3 accounts, 5 categories, 42 transactions, and 2 budgets/,
      ),
    ).toBeInTheDocument()
    // The dialog closes on success rather than lingering.
    expect(
      screen.queryByRole('button', { name: 'Replace everything' }),
    ).not.toBeInTheDocument()
  })

  it('keeps the dialog open and shows the error inline when restore fails', async () => {
    const user = userEvent.setup()
    mockedRestoreSnapshot.mockRejectedValue(
      new ApiError('invalid_input', 'That backup is missing a format version.'),
    )
    renderWithRouter()

    await user.upload(screen.getByLabelText('Backup file'), backupFile())
    await user.click(
      await screen.findByRole('button', { name: /Replace everything…/ }),
    )
    await user.type(screen.getByLabelText(/Type .* to confirm/), CONFIRM_PHRASE)
    fireEvent.click(screen.getByRole('button', { name: 'Replace everything' }))

    expect(
      await screen.findByText('That backup is missing a format version.'),
    ).toBeInTheDocument()
    // Still open — a failed restore doesn't lose the confirmation state.
    expect(
      screen.getByRole('button', { name: 'Replace everything' }),
    ).toBeInTheDocument()
  })
})
