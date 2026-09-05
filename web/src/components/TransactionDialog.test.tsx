// Component tests for the create side of TransactionDialog (issue #60's
// fast-entry budget, now via a dialog instead of a separate page) and its
// "Enter multiple" toggle. Edit-mode behaviour (pre-filling, notes/tags
// round-tripping, the immutable kind label) is covered as a real
// integration through TransactionsList.test.tsx instead of duplicated
// here, since that's the only place edit is actually triggered from.
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
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

  // Regression test for a bug found via direct user report (no filed
  // issue): TransactionDialogProvider keys TransactionDialogSheet as
  // 'create' for every create open, so a second openCreate() reuses the
  // same instance — every field's useState(initial) initializer, and
  // the accounts/categories fetch effect, had already run once and
  // never ran again, leaving whatever was last typed/fetched behind.
  it('resets every field when the create dialog is closed and reopened', async () => {
    renderAndOpen()
    await screen.findByText('HDFC Savings')

    fireEvent.change(screen.getByLabelText('Amount'), {
      target: { value: '800' },
    })
    fireEvent.change(screen.getByLabelText('Category'), {
      target: { value: 'cat-1' },
    })
    fireEvent.click(screen.getByText('Add details'))
    fireEvent.change(screen.getByLabelText('Description'), {
      target: { value: 'Leftover text' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    )

    fireEvent.click(screen.getByText('Open'))
    await screen.findByText('HDFC Savings')

    expect(screen.getByLabelText('Amount')).toHaveValue('')
    expect(screen.getByLabelText('Category')).toHaveValue('')
    expect(screen.queryByLabelText('Description')).not.toBeInTheDocument()
    expect(screen.getByText('Add details')).toBeInTheDocument()
  })

  // Same root cause as above, for the accounts/categories fetch: it was
  // gated on a mount-only effect, so anything added via Settings between
  // two opens of the same reused dialog instance never showed up.
  it('re-fetches accounts and categories on reopen, picking up anything added since', async () => {
    renderAndOpen()
    await screen.findByText('HDFC Savings')
    expect(screen.queryByText('Rent')).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument(),
    )

    mockedListCategories.mockResolvedValue([
      category,
      { ...category, id: 'cat-2', name: 'Rent' },
    ])

    fireEvent.click(screen.getByText('Open'))
    await screen.findByText('HDFC Savings')

    expect(mockedListCategories).toHaveBeenCalledTimes(2)
    expect(
      within(screen.getByLabelText('Category')).getByRole('option', {
        name: 'Rent',
      }),
    ).toBeInTheDocument()
  })

  it('picks a date via the Radix date picker in "Add details" (issue #104)', async () => {
    const user = userEvent.setup()
    renderAndOpen()
    await screen.findByText('HDFC Savings')

    fireEvent.click(screen.getByText('Add details'))
    // The date picker button's accessible name comes from its
    // <Label htmlFor="td-date">, not its own "Pick a date" placeholder
    // text — buttons are labelable elements, and an explicit label
    // association takes precedence over content in accessible-name
    // computation.
    await user.click(screen.getByRole('button', { name: 'Date' }))
    await user.click(await screen.findByRole('button', { name: /15th, 2026/ }))

    fireEvent.change(screen.getByLabelText('Amount'), {
      target: { value: '800' },
    })
    fireEvent.change(screen.getByLabelText('Category'), {
      target: { value: 'cat-1' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Record spend' }))

    await waitFor(() =>
      expect(mockedRecordOutflow).toHaveBeenCalledWith(
        expect.objectContaining({ date: expect.stringMatching(/-15$/) }),
      ),
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
