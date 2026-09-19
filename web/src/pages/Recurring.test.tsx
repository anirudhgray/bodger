// Component tests for the recurring screen (issue #282). lib/api's
// listRecurringRules/listScheduledOccurrences/listAccounts/listCategories/
// createRecurringRule/updateRecurringRule/archiveRecurringRule/
// refreshOccurrences/materialiseOccurrence/skipOccurrence are mocked so
// these exercise only the screen's own rendering and orchestration — not
// apiFetch's own behaviour (covered in api.test.ts). Mirrors
// Budgets.test.tsx's own shape closely.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api')
  return {
    ...actual,
    listRecurringRules: vi.fn(),
    listScheduledOccurrences: vi.fn(),
    listAccounts: vi.fn(),
    listCategories: vi.fn(),
    createRecurringRule: vi.fn(),
    updateRecurringRule: vi.fn(),
    archiveRecurringRule: vi.fn(),
    refreshOccurrences: vi.fn(),
    materialiseOccurrence: vi.fn(),
    skipOccurrence: vi.fn(),
  }
})

import {
  ApiError,
  archiveRecurringRule,
  createRecurringRule,
  listAccounts,
  listCategories,
  listRecurringRules,
  listScheduledOccurrences,
  materialiseOccurrence,
  refreshOccurrences,
  skipOccurrence,
  type Account,
  type Category,
  type MaterialiseOccurrence,
  type RecurringRule,
  type ScheduledOccurrence,
} from '@/lib/api'
import { RecurringPage } from './Recurring'

const mockedListRecurringRules = vi.mocked(listRecurringRules)
const mockedListScheduledOccurrences = vi.mocked(listScheduledOccurrences)
const mockedListAccounts = vi.mocked(listAccounts)
const mockedListCategories = vi.mocked(listCategories)
const mockedCreateRecurringRule = vi.mocked(createRecurringRule)
const mockedArchiveRecurringRule = vi.mocked(archiveRecurringRule)
const mockedRefreshOccurrences = vi.mocked(refreshOccurrences)
const mockedMaterialiseOccurrence = vi.mocked(materialiseOccurrence)
const mockedSkipOccurrence = vi.mocked(skipOccurrence)

const accounts: Account[] = [
  {
    id: 'a1',
    name: 'Checking',
    type: 'bank',
    currency: 'USD',
    opening_balance: '0.00',
    sort_order: 0,
    archived: false,
  },
]

const categories: Category[] = [
  { id: 'c1', name: 'Rent', type: 'expense', sort_order: 0, archived: false },
]

const rentRule: RecurringRule = {
  id: 'r1',
  account_id: 'a1',
  category_id: 'c1',
  amount: '1200.00',
  currency: 'USD',
  archived: false,
  description: 'Rent',
  starts_on: '2026-01-01',
  schedule: { frequency: 'monthly', interval: 1, day_of_month: 1 },
}

const pendingOccurrence: ScheduledOccurrence = {
  id: 'o1',
  rule_id: 'r1',
  occurrence_date: '2026-10-01',
  status: 'pending',
}

function renderPage() {
  return render(
    <MemoryRouter>
      <RecurringPage />
    </MemoryRouter>,
  )
}

describe('RecurringPage', () => {
  beforeEach(() => {
    mockedListRecurringRules.mockReset()
    mockedListScheduledOccurrences.mockReset()
    mockedListAccounts.mockReset().mockResolvedValue(accounts)
    mockedListCategories.mockReset().mockResolvedValue(categories)
    mockedCreateRecurringRule.mockReset()
    mockedArchiveRecurringRule.mockReset()
    mockedRefreshOccurrences.mockReset()
    mockedMaterialiseOccurrence.mockReset()
    mockedSkipOccurrence.mockReset()
  })

  it('renders a rule with its schedule summary and amount', async () => {
    mockedListRecurringRules.mockResolvedValue([rentRule])
    mockedListScheduledOccurrences.mockResolvedValue([])

    renderPage()

    expect(await screen.findByText('Rent')).toBeInTheDocument()
    expect(
      screen.getByText(/Checking · Rent · Every month on the 1st/),
    ).toBeInTheDocument()
    expect(screen.getByText('−1200.00 USD')).toBeInTheDocument()
  })

  it('shows an empty state with a call to action when there are no rules', async () => {
    mockedListRecurringRules.mockResolvedValue([])
    mockedListScheduledOccurrences.mockResolvedValue([])

    renderPage()

    expect(
      await screen.findByText('No recurring rules yet'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'New rule' })).toBeInTheDocument()
  })

  it('shows an inline error when the rule list fails to load', async () => {
    mockedListRecurringRules.mockRejectedValue(new ApiError('internal', 'boom'))
    mockedListScheduledOccurrences.mockResolvedValue([])

    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('boom')
  })

  it('renders a pending occurrence in its own distinctly-styled section, structurally apart from the rules list', async () => {
    mockedListRecurringRules.mockResolvedValue([rentRule])
    mockedListScheduledOccurrences.mockResolvedValue([pendingOccurrence])

    renderPage()

    expect(await screen.findByText('Upcoming (projected)')).toBeInTheDocument()
    expect(screen.getByText('Projected')).toBeInTheDocument()
    expect(screen.getByText(/2026-10-01/)).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Materialise' }),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Skip' })).toBeInTheDocument()
  })

  it('refreshes occurrences for every active rule via the page-level button', async () => {
    mockedListRecurringRules.mockResolvedValue([rentRule])
    mockedListScheduledOccurrences.mockResolvedValue([])
    mockedRefreshOccurrences.mockResolvedValue({ created: [pendingOccurrence] })

    renderPage()
    await screen.findByText('Rent')

    fireEvent.click(screen.getByRole('button', { name: 'Refresh occurrences' }))

    await waitFor(() => expect(mockedRefreshOccurrences).toHaveBeenCalledWith())
    expect(mockedListScheduledOccurrences).toHaveBeenCalledTimes(2)
  })

  it('materialises a pending occurrence, then reloads the list', async () => {
    mockedListRecurringRules.mockResolvedValue([rentRule])
    mockedListScheduledOccurrences
      .mockResolvedValueOnce([pendingOccurrence])
      .mockResolvedValueOnce([])
    const materialised: MaterialiseOccurrence = {
      occurrence: {
        ...pendingOccurrence,
        status: 'materialised',
        transaction_id: 't1',
      },
      transaction: {
        id: 't1',
        type: 'outflow',
        date: '2026-10-01',
        description: 'Rent',
        amount: '1200.00',
        currency: 'USD',
      },
    }
    mockedMaterialiseOccurrence.mockResolvedValue(materialised)

    renderPage()
    await screen.findByText('Upcoming (projected)')

    fireEvent.click(screen.getByRole('button', { name: 'Materialise' }))

    await waitFor(() =>
      expect(mockedMaterialiseOccurrence).toHaveBeenCalledWith('o1'),
    )
    await waitFor(() =>
      expect(
        screen.getByText(
          'Nothing pending. Use “Refresh occurrences” above to generate upcoming dates from your active rules.',
        ),
      ).toBeInTheDocument(),
    )
  })

  it('skips a pending occurrence', async () => {
    mockedListRecurringRules.mockResolvedValue([rentRule])
    mockedListScheduledOccurrences
      .mockResolvedValueOnce([pendingOccurrence])
      .mockResolvedValueOnce([])
    mockedSkipOccurrence.mockResolvedValue({
      ...pendingOccurrence,
      status: 'skipped',
    })

    renderPage()
    await screen.findByText('Upcoming (projected)')

    fireEvent.click(screen.getByRole('button', { name: 'Skip' }))

    await waitFor(() => expect(mockedSkipOccurrence).toHaveBeenCalledWith('o1'))
  })

  it('archives a rule', async () => {
    mockedListRecurringRules
      .mockResolvedValueOnce([rentRule])
      .mockResolvedValueOnce([])
    mockedListScheduledOccurrences.mockResolvedValue([])
    mockedArchiveRecurringRule.mockResolvedValue({
      ...rentRule,
      archived: true,
    })

    renderPage()
    await screen.findByText('Rent')

    fireEvent.click(screen.getByRole('button', { name: 'Archive' }))

    await waitFor(() =>
      expect(mockedArchiveRecurringRule).toHaveBeenCalledWith('r1'),
    )
    await waitFor(() =>
      expect(screen.getByText('No recurring rules yet')).toBeInTheDocument(),
    )
  })

  it('creates a rule through the dialog', async () => {
    const user = userEvent.setup()
    mockedListRecurringRules
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([rentRule])
    mockedListScheduledOccurrences.mockResolvedValue([])
    mockedCreateRecurringRule.mockResolvedValue(rentRule)

    renderPage()
    await screen.findByText('No recurring rules yet')

    await user.click(screen.getByRole('button', { name: 'New rule' }))
    const dialog = screen.getByRole('dialog', { name: 'New recurring rule' })

    await user.type(
      dialog.querySelector('#rule-description') as HTMLInputElement,
      'Rent',
    )
    await user.click(dialog.querySelector('#rule-account') as HTMLElement)
    await user.click(await screen.findByRole('option', { name: 'Checking' }))
    await user.click(dialog.querySelector('#rule-category') as HTMLElement)
    await user.click(await screen.findByRole('option', { name: 'Rent' }))
    await user.type(
      dialog.querySelector('#rule-amount') as HTMLInputElement,
      '1200.00',
    )
    await user.type(
      dialog.querySelector('#rule-day-of-month') as HTMLInputElement,
      '{selectall}1',
    )

    await user.click(screen.getByRole('button', { name: 'Create rule' }))

    await waitFor(() =>
      expect(mockedCreateRecurringRule).toHaveBeenCalledWith(
        expect.objectContaining({
          accountRef: 'a1',
          categoryRef: 'c1',
          amount: '1200.00',
          description: 'Rent',
          schedule: expect.objectContaining({ frequency: 'monthly' }),
        }),
      ),
    )
  })
})
