// Component tests for the budgets screen (issue #245). lib/api's
// listBudgets/listCategories/getBudgetHistory/archiveBudget/createBudget/
// updateBudget/addBudgetLine/updateBudgetLine/removeBudgetLine are mocked
// so these exercise only the screen's own rendering and orchestration —
// loading, the real per-budget actual-vs-budget table with its utilisation
// label, an empty ledger, a server error, previous/next period navigation,
// and the create/edit dialog's line-diffing (BudgetDialog's
// applyLineChanges) — not apiFetch's own behaviour (covered in api.test.ts).
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    listBudgets: vi.fn(),
    listCategories: vi.fn(),
    getBudgetHistory: vi.fn(),
    archiveBudget: vi.fn(),
    createBudget: vi.fn(),
    updateBudget: vi.fn(),
    addBudgetLine: vi.fn(),
    updateBudgetLine: vi.fn(),
    removeBudgetLine: vi.fn(),
  }
})

import {
  ApiError,
  addBudgetLine,
  archiveBudget,
  createBudget,
  getBudgetHistory,
  listBudgets,
  listCategories,
  removeBudgetLine,
  updateBudget,
  updateBudgetLine,
  type Budget,
  type BudgetHistory,
  type Category,
} from '@/lib/api'
import { BudgetsPage } from './Budgets'

const mockedListBudgets = vi.mocked(listBudgets)
const mockedListCategories = vi.mocked(listCategories)
const mockedGetBudgetHistory = vi.mocked(getBudgetHistory)
const mockedArchiveBudget = vi.mocked(archiveBudget)
const mockedCreateBudget = vi.mocked(createBudget)
const mockedUpdateBudget = vi.mocked(updateBudget)
const mockedAddBudgetLine = vi.mocked(addBudgetLine)
const mockedUpdateBudgetLine = vi.mocked(updateBudgetLine)
const mockedRemoveBudgetLine = vi.mocked(removeBudgetLine)

const categories: Category[] = [
  {
    id: 'c1',
    name: 'Groceries',
    type: 'expense',
    sort_order: 0,
    archived: false,
  },
  { id: 'c2', name: 'Rent', type: 'expense', sort_order: 1, archived: false },
]

const household: Budget = {
  id: 'b1',
  name: 'Household',
  period_type: 'monthly',
  currency: 'USD',
  starts_on: '2026-01-01',
  archived: false,
  lines: [{ id: 'l1', category_id: 'c1', amount: '500.00', rollover: false }],
}

// asOf defaults to `from`, outside most tests' concern — pass it
// explicitly to exercise the month-progress marker (monthProgressPercent
// in Budgets.tsx), which only renders when as_of falls within [from, to].
function historyFor(
  budget: Budget,
  from: string,
  to: string,
  asOf: string = from,
): BudgetHistory {
  return {
    budget_id: budget.id,
    periods: [
      {
        budget_id: budget.id,
        currency: budget.currency,
        from,
        to,
        as_of: asOf,
        overall: {
          budgeted: budget.lines
            .reduce((sum, l) => sum + Number(l.amount), 0)
            .toFixed(2),
          actual: (450 * budget.lines.length).toFixed(2),
          remaining: '50.00',
          utilisation: 0.9,
        },
        lines: budget.lines.map((l) => ({
          line_id: l.id,
          category_id: l.category_id,
          budgeted: l.amount,
          actual: '450.00',
          remaining: '50.00',
          utilisation: 0.9,
        })),
        unconverted: [],
      },
    ],
  }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <BudgetsPage />
    </MemoryRouter>,
  )
}

describe('BudgetsPage', () => {
  beforeEach(() => {
    mockedListBudgets.mockReset()
    mockedListCategories.mockReset().mockResolvedValue(categories)
    // A harmless default so a test that doesn't care about period data
    // (e.g. the create-budget flow, which only reaches this fetch after
    // its own explicit listBudgets refresh) doesn't crash on an
    // unconfigured mock — individual tests override this with real data
    // where the period table's own content matters.
    mockedGetBudgetHistory.mockReset().mockResolvedValue({
      budget_id: '',
      periods: [],
    })
    mockedArchiveBudget.mockReset()
    mockedCreateBudget.mockReset()
    mockedUpdateBudget.mockReset()
    mockedAddBudgetLine.mockReset()
    mockedUpdateBudgetLine.mockReset()
    mockedRemoveBudgetLine.mockReset()
  })

  it('renders a budget’s actual-vs-budget line with its utilisation label', async () => {
    mockedListBudgets.mockResolvedValue([household])
    mockedGetBudgetHistory.mockResolvedValue(
      historyFor(household, '2026-09-01', '2026-09-30'),
    )

    renderPage()

    // "Household" (the budget's own name) renders as soon as listBudgets
    // resolves, a render ahead of the per-period getBudgetHistory fetch —
    // await the line-level data itself so this doesn't race that second,
    // independent async step.
    expect(await screen.findByText('Groceries')).toBeInTheDocument()
    expect(screen.getByText('Household')).toBeInTheDocument()
    expect(screen.getByText('500.00 USD')).toBeInTheDocument()
    expect(screen.getByText('450.00 USD')).toBeInTheDocument()
    expect(screen.getByText('50.00 USD')).toBeInTheDocument()
    // "90%"/"At budget" render twice — once for the Overall row, once for
    // the one line — because with a single line, Overall trivially equals
    // it (TestBudgetActuals_OverallSumsAllLines in the Go suite covers the
    // case where they diverge, with more than one line).
    expect(screen.getAllByText('90%')).toHaveLength(2)
    expect(screen.getAllByText('At budget')).toHaveLength(2)
    expect(screen.getByText('September 2026')).toBeInTheDocument()
    expect(mockedGetBudgetHistory).toHaveBeenCalledWith('b1', undefined, 1)
  })

  it('shows the month-progress marker when the period is the current one', async () => {
    mockedListBudgets.mockResolvedValue([household])
    // 13th of a 30-day September: 13/30 = 43.3%, rounds to 43%.
    mockedGetBudgetHistory.mockResolvedValue(
      historyFor(household, '2026-09-01', '2026-09-30', '2026-09-13'),
    )

    renderPage()

    expect(
      await screen.findByText(/43% of the way through/),
    ).toBeInTheDocument()
  })

  it('hides the month-progress marker for a period that is not the current one', async () => {
    mockedListBudgets.mockResolvedValue([household])
    // as_of (today) is in August; the period being viewed is September —
    // a past or future period gets no marker, only the current one does.
    mockedGetBudgetHistory.mockResolvedValue(
      historyFor(household, '2026-09-01', '2026-09-30', '2026-08-15'),
    )

    renderPage()

    await screen.findByText('Household')
    expect(screen.queryByText(/of the way through/)).not.toBeInTheDocument()
  })

  it('shows an empty state with a call to action when there are no budgets', async () => {
    mockedListBudgets.mockResolvedValue([])

    renderPage()

    expect(await screen.findByText('No budgets yet')).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'New budget' }),
    ).toBeInTheDocument()
  })

  it('shows an inline error when the budget list fails to load', async () => {
    mockedListBudgets.mockRejectedValue(new ApiError('internal', 'boom'))

    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('boom')
  })

  it('pages to the previous month via the history endpoint', async () => {
    mockedListBudgets.mockResolvedValue([household])
    mockedGetBudgetHistory.mockResolvedValue(
      historyFor(household, '2026-09-01', '2026-09-30'),
    )

    renderPage()
    expect(await screen.findByText('September 2026')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Previous' }))

    await waitFor(() =>
      expect(mockedGetBudgetHistory).toHaveBeenLastCalledWith(
        'b1',
        '2026-08-01',
        1,
      ),
    )
  })

  it('creates a budget with a new line through the dialog', async () => {
    const user = userEvent.setup()
    mockedListBudgets
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([household])
    mockedCreateBudget.mockResolvedValue(household)

    renderPage()
    await screen.findByText('No budgets yet')

    fireEvent.click(screen.getByRole('button', { name: 'New budget' }))
    expect(
      await screen.findByRole('dialog', { name: 'New budget' }),
    ).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Name'), {
      target: { value: 'Groceries Budget' },
    })
    fireEvent.click(screen.getByRole('button', { name: /Add line/ }))

    await user.click(screen.getByRole('combobox', { name: 'Category' }))
    await user.click(await screen.findByRole('option', { name: 'Groceries' }))
    fireEvent.change(screen.getByLabelText('Amount'), {
      target: { value: '500' },
    })

    fireEvent.click(screen.getByRole('button', { name: 'Create budget' }))

    await waitFor(() =>
      expect(mockedCreateBudget).toHaveBeenCalledWith({
        name: 'Groceries Budget',
        currency: undefined,
        startsOn: undefined,
        lines: [{ categoryRef: 'c1', amount: '500', rollover: false }],
      }),
    )
  })

  it('diffs line changes on save: updates one line and removes another', async () => {
    const twoLineBudget: Budget = {
      ...household,
      lines: [
        { id: 'l1', category_id: 'c1', amount: '500.00', rollover: false },
        { id: 'l2', category_id: 'c2', amount: '1000.00', rollover: false },
      ],
    }
    mockedListBudgets.mockResolvedValue([twoLineBudget])
    mockedGetBudgetHistory.mockResolvedValue(
      historyFor(twoLineBudget, '2026-09-01', '2026-09-30'),
    )
    mockedUpdateBudget.mockResolvedValue(twoLineBudget)
    mockedUpdateBudgetLine.mockResolvedValue(twoLineBudget)
    mockedRemoveBudgetLine.mockResolvedValue(twoLineBudget)

    renderPage()
    await screen.findByText('Household')

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }))
    expect(
      await screen.findByRole('dialog', { name: 'Edit budget' }),
    ).toBeInTheDocument()

    const amountInputs = screen.getAllByLabelText('Amount')
    fireEvent.change(amountInputs[0], { target: { value: '600' } })

    const removeButtons = screen.getAllByRole('button', { name: 'Remove line' })
    fireEvent.click(removeButtons[1])

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() =>
      expect(mockedUpdateBudget).toHaveBeenCalledWith('b1', {
        name: 'Household',
        startsOn: '2026-01-01',
      }),
    )
    expect(mockedUpdateBudgetLine).toHaveBeenCalledWith('b1', 'l1', {
      amount: '600',
      rollover: false,
    })
    expect(mockedRemoveBudgetLine).toHaveBeenCalledWith('b1', 'l2')
    expect(mockedAddBudgetLine).not.toHaveBeenCalled()
  })

  it('archives a budget and refreshes the list', async () => {
    mockedListBudgets
      .mockResolvedValueOnce([household])
      .mockResolvedValueOnce([])
    mockedGetBudgetHistory.mockResolvedValue(
      historyFor(household, '2026-09-01', '2026-09-30'),
    )
    mockedArchiveBudget.mockResolvedValue({ ...household, archived: true })

    renderPage()
    await screen.findByText('Household')

    fireEvent.click(screen.getByRole('button', { name: 'Archive' }))

    await waitFor(() => expect(mockedArchiveBudget).toHaveBeenCalledWith('b1'))
    expect(await screen.findByText('No budgets yet')).toBeInTheDocument()
  })
})
