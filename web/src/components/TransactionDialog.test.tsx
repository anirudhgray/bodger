// Component tests for the create side of TransactionDialog (issue #60's
// fast-entry budget, now via a dialog instead of a separate page) and its
// "Enter multiple" toggle. Edit-mode behaviour (pre-filling, notes/tags
// round-tripping, the immutable kind label) is covered as a real
// integration through TransactionsList.test.tsx instead of duplicated
// here, since that's the only place edit is actually triggered from.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    listAccounts: vi.fn(),
    listCategories: vi.fn(),
    recordOutflow: vi.fn(),
    recordInflow: vi.fn(),
    recordTransfer: vi.fn(),
  }
})

import {
  ApiError,
  listAccounts,
  listCategories,
  recordOutflow,
  type Account,
  type Category,
  type Transaction,
} from '@/lib/api'
import {
  TransactionDialogProvider,
  useTransactionDialog,
} from './TransactionDialog'

const mockedListAccounts = vi.mocked(listAccounts)
const mockedListCategories = vi.mocked(listCategories)
const mockedRecordOutflow = vi.mocked(recordOutflow)

const account: Account = {
  id: 'acc-1',
  name: 'HDFC Savings',
  type: 'bank',
  currency: 'INR',
  opening_balance: '0.00',
  sort_order: 0,
  archived: false,
}

const category: Category = {
  id: 'cat-1',
  name: 'Groceries',
  type: 'expense',
  sort_order: 0,
  archived: false,
}

const recorded: Transaction = {
  id: 't1',
  type: 'outflow',
  date: '2026-09-05',
  description: 'Groceries',
  account_id: 'acc-1',
  category_id: 'cat-1',
  amount: '800',
  currency: 'INR',
}

function Harness({ onSaved }: { onSaved?: (t: Transaction) => void }) {
  const { openCreate } = useTransactionDialog()
  return (
    <button type="button" onClick={() => openCreate(onSaved)}>
      Open
    </button>
  )
}

function renderAndOpen(onSaved?: (t: Transaction) => void) {
  render(
    <TransactionDialogProvider>
      <Harness onSaved={onSaved} />
    </TransactionDialogProvider>,
  )
  fireEvent.click(screen.getByText('Open'))
}

describe('TransactionDialog (create)', () => {
  beforeEach(() => {
    mockedListAccounts.mockReset().mockResolvedValue([account])
    mockedListCategories.mockReset().mockResolvedValue([category])
    mockedRecordOutflow.mockReset().mockResolvedValue(recorded)
  })

  it('records a spend and calls onSaved, closing the dialog by default', async () => {
    const onSaved = vi.fn()
    renderAndOpen(onSaved)
    await screen.findByText('HDFC Savings')

    fireEvent.change(screen.getByLabelText('Amount'), {
      target: { value: '800' },
    })
    fireEvent.change(screen.getByLabelText('Category'), {
      target: { value: 'cat-1' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Record spend' }))

    await waitFor(() =>
      expect(mockedRecordOutflow).toHaveBeenCalledWith(
        expect.objectContaining({
          account: 'acc-1',
          category: 'cat-1',
          amount: '800',
        }),
      ),
    )
    await waitFor(() => expect(onSaved).toHaveBeenCalledWith(recorded))
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    )
  })

  it('keeps the submit button disabled until amount and category are filled in', async () => {
    renderAndOpen()
    await screen.findByText('HDFC Savings')

    expect(screen.getByRole('button', { name: 'Record spend' })).toBeDisabled()
  })

  it('rejects non-numeric characters in the amount field as they are typed', async () => {
    renderAndOpen()
    await screen.findByText('HDFC Savings')

    fireEvent.change(screen.getByLabelText('Amount'), {
      target: { value: 'soemthing' },
    })
    expect(screen.getByLabelText('Amount')).toHaveValue('')

    fireEvent.change(screen.getByLabelText('Amount'), {
      target: { value: '$800' },
    })
    expect(screen.getByLabelText('Amount')).toHaveValue('800')
  })

  it('switches to a Move form with from/to accounts and no category field', async () => {
    mockedListAccounts.mockResolvedValue([
      account,
      { ...account, id: 'acc-2', name: 'Checking' },
    ])
    renderAndOpen()
    await screen.findByText('HDFC Savings')

    // TabsTrigger activates on mousedown (or focus), not on the
    // synthetic 'click' event fireEvent.click dispatches alone.
    await screen.findByRole('tab', { name: 'Move' })
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'Move' }))

    expect(screen.queryByLabelText('Category')).not.toBeInTheDocument()
    expect(screen.getByLabelText('From account')).toBeInTheDocument()
    expect(screen.getByLabelText('To account')).toBeInTheDocument()
  })

  it('preserves what was typed when the API rejects the submission', async () => {
    mockedRecordOutflow.mockRejectedValue(
      new ApiError(
        'invalid_input',
        "Couldn't find a category called 'cat-1'.",
        'category',
      ),
    )
    renderAndOpen()
    await screen.findByText('HDFC Savings')

    fireEvent.change(screen.getByLabelText('Amount'), {
      target: { value: '800' },
    })
    fireEvent.change(screen.getByLabelText('Category'), {
      target: { value: 'cat-1' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Record spend' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      "Couldn't find a category called 'cat-1'.",
    )
    expect(screen.getByLabelText('Amount')).toHaveValue('800')
    expect(screen.getByLabelText('Category')).toHaveValue('cat-1')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('"Enter multiple" keeps the dialog open and resets the form after a save', async () => {
    const onSaved = vi.fn()
    renderAndOpen(onSaved)
    await screen.findByText('HDFC Savings')

    fireEvent.click(screen.getByRole('switch', { name: 'Enter multiple' }))
    fireEvent.change(screen.getByLabelText('Amount'), {
      target: { value: '800' },
    })
    fireEvent.change(screen.getByLabelText('Category'), {
      target: { value: 'cat-1' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Record spend' }))

    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(await screen.findByRole('status')).toHaveTextContent('Recorded.')
    expect(screen.getByLabelText('Amount')).toHaveValue('')

    // The account stays put (the next entry is likely the same account)
    // but category/amount reset, and the switch itself stays on.
    expect(screen.getByRole('switch', { name: 'Enter multiple' })).toBeChecked()
  })
})
