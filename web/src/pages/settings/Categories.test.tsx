// Component tests for the Categories settings subpage (issue #107, split
// out of the old Settings.test.tsx; issue #106 added the tree display
// and swapped the "Parent" field for a Combobox). web/src/lib/settings
// and web/src/lib/api (listCategories lives there — issue #183) are
// mocked here so these exercise only the page's own logic against a
// controlled fake API. The Parent field is a real interaction target
// now, not a native <select> — see combobox.test.tsx and
// TransactionDialog.test.tsx's pickCategory for the same convention.
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, it, expect, vi } from 'vitest'

vi.mock('@/lib/settings', () => ({
  createCategory: vi.fn(),
  renameCategory: vi.fn(),
  archiveCategory: vi.fn(),
  reparentCategory: vi.fn(),
}))

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    listCategories: vi.fn(),
  }
})

// Create/rename/archive/reparent are user-triggered actions, so their
// failures now report via a toast rather than inline (docs/design-
// system.md's "Toasts vs. inline messages") — mocked here so tests can
// assert on what was shown without a real <Toaster/> mounted.
vi.mock('sonner', () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    promise: vi.fn(),
  },
}))

import { ApiError, listCategories } from '@/lib/api'
import {
  archiveCategory,
  createCategory,
  reparentCategory,
} from '@/lib/settings'
import { CategoriesSettings } from './Categories'
import { toast } from 'sonner'

const mockedListCategories = vi.mocked(listCategories)
const mockedReparentCategory = vi.mocked(reparentCategory)
const mockedCreateCategory = vi.mocked(createCategory)
const mockedArchiveCategory = vi.mocked(archiveCategory)
const mockedToastError = vi.mocked(toast.error)
const mockedToastPromise = vi.mocked(toast.promise)

describe('CategoriesSettings', () => {
  beforeEach(() => {
    mockedListCategories.mockReset().mockResolvedValue([])
    mockedReparentCategory.mockReset()
    mockedCreateCategory.mockReset()
    mockedArchiveCategory.mockReset()
    mockedToastError.mockReset()
    mockedToastPromise.mockReset()
  })

  it('only offers same-kind categories as a Parent option', async () => {
    const user = userEvent.setup()
    mockedListCategories.mockReset().mockResolvedValue([
      {
        id: 'cat_dining',
        name: 'Dining',
        type: 'expense',
        sort_order: 0,
        archived: false,
      },
      {
        id: 'cat_groceries',
        name: 'Groceries',
        type: 'expense',
        sort_order: 1,
        archived: false,
      },
      {
        id: 'cat_salary',
        name: 'Salary',
        type: 'income',
        sort_order: 0,
        archived: false,
      },
    ])
    render(<CategoriesSettings />)

    const parentCombobox = await screen.findByLabelText('Parent', {
      selector: '#parent-cat_dining',
    })
    await user.click(parentCombobox)

    // Dining (expense) may become top-level or sit under Groceries
    // (expense), but never under Salary (income) — one typed tree,
    // data-model.md §6.
    expect(
      screen.getByRole('option', { name: 'Top level' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('option', { name: 'Groceries' }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('option', { name: 'Salary' }),
    ).not.toBeInTheDocument()
  })

  it('indents a nested category under its parent in the list (issue #106)', async () => {
    mockedListCategories.mockReset().mockResolvedValue([
      {
        id: 'cat_food',
        name: 'Food',
        type: 'expense',
        sort_order: 0,
        archived: false,
      },
      {
        id: 'cat_groceries',
        name: 'Groceries',
        type: 'expense',
        sort_order: 0,
        archived: false,
        parent_id: 'cat_food',
      },
    ])
    render(<CategoriesSettings />)

    // Scoped to the <p> holding each row's own name — "Food" also shows
    // up a second time as Groceries' own Parent combobox trigger text.
    const foodRow = (
      await screen.findByText('Food', { selector: 'p' })
    ).closest('li')
    const groceriesRow = screen
      .getByText('Groceries', { selector: 'p' })
      .closest('li')
    expect(foodRow).not.toBeNull()
    expect(groceriesRow).not.toBeNull()

    // The child's row sits further right than its parent's — the visual
    // nesting affordance this issue asks for, not just a flat list in
    // parent_id-happens-to-be-adjacent order.
    const foodPaddingLeft = parseInt(
      (foodRow as HTMLElement).style.paddingLeft,
      10,
    )
    const groceriesPaddingLeft = parseInt(
      (groceriesRow as HTMLElement).style.paddingLeft,
      10,
    )
    expect(groceriesPaddingLeft).toBeGreaterThan(foodPaddingLeft)
  })

  it('shows a nested category’s full path in its own Parent combobox once opened', async () => {
    const user = userEvent.setup()
    mockedListCategories.mockReset().mockResolvedValue([
      {
        id: 'cat_food',
        name: 'Food',
        type: 'expense',
        sort_order: 0,
        archived: false,
      },
      {
        id: 'cat_groceries',
        name: 'Groceries',
        type: 'expense',
        sort_order: 0,
        archived: false,
        parent_id: 'cat_food',
      },
      {
        id: 'cat_produce',
        name: 'Produce',
        type: 'expense',
        sort_order: 0,
        archived: false,
        parent_id: 'cat_groceries',
      },
    ])
    render(<CategoriesSettings />)

    // Produce's own Parent field already shows Groceries selected —
    // reflected as the full "Food > Groceries" ancestry, not just
    // "Groceries" on its own, since a same-named category elsewhere in
    // the tree would otherwise be indistinguishable.
    const produceParent = await screen.findByLabelText('Parent', {
      selector: '#parent-cat_produce',
    })
    expect(produceParent).toHaveTextContent('Food > Groceries')

    await user.click(produceParent)
    expect(
      screen.getByRole('option', { name: 'Groceries' }),
    ).toBeInTheDocument()
  })

  it('reparents a category by picking a new parent from the combobox', async () => {
    const user = userEvent.setup()
    mockedListCategories.mockReset().mockResolvedValueOnce([
      {
        id: 'cat_food',
        name: 'Food',
        type: 'expense',
        sort_order: 0,
        archived: false,
      },
      {
        id: 'cat_dining',
        name: 'Dining',
        type: 'expense',
        sort_order: 1,
        archived: false,
      },
    ])
    mockedReparentCategory.mockResolvedValue({
      id: 'cat_dining',
      name: 'Dining',
      type: 'expense',
      sort_order: 1,
      archived: false,
      parent_id: 'cat_food',
    })
    mockedListCategories.mockResolvedValueOnce([
      {
        id: 'cat_food',
        name: 'Food',
        type: 'expense',
        sort_order: 0,
        archived: false,
      },
      {
        id: 'cat_dining',
        name: 'Dining',
        type: 'expense',
        sort_order: 1,
        archived: false,
        parent_id: 'cat_food',
      },
    ])
    render(<CategoriesSettings />)

    const diningParent = await screen.findByLabelText('Parent', {
      selector: '#parent-cat_dining',
    })
    await user.click(diningParent)
    await user.click(await screen.findByRole('option', { name: 'Food' }))

    expect(mockedReparentCategory).toHaveBeenCalledWith(
      'cat_dining',
      'cat_food',
    )
  })

  it('shows a toast (not inline) when creating a category fails', async () => {
    mockedCreateCategory.mockRejectedValue(
      new ApiError(
        'invalid_input',
        'A category named "Dining" already exists.',
      ),
    )
    render(<CategoriesSettings />)
    await screen.findByRole('button', { name: 'Add' })

    fireEvent.change(screen.getByLabelText('New category name'), {
      target: { value: 'Dining' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Add' }))

    // Creating a category is a user-triggered action (docs/design-
    // system.md's "Toasts vs. inline messages"), so its failure is
    // reported via a toast, not inline.
    await waitFor(() =>
      expect(mockedToastError).toHaveBeenCalledWith(
        'A category named "Dining" already exists.',
      ),
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('leaves the category in place and reports a toast (not inline) when archive fails', async () => {
    mockedListCategories.mockResolvedValue([
      {
        id: 'cat_dining',
        name: 'Dining',
        type: 'expense',
        sort_order: 0,
        archived: false,
      },
    ])
    mockedArchiveCategory.mockRejectedValue(
      new ApiError('internal', 'Couldn’t reach the server. Try again.'),
    )
    render(<CategoriesSettings />)

    const archiveButton = await screen.findByRole('button', {
      name: 'Archive',
    })
    fireEvent.click(archiveButton)

    await waitFor(() => expect(mockedToastPromise).toHaveBeenCalled())
    // Archiving a category is a user-triggered action (docs/design-
    // system.md's "Toasts vs. inline messages"), so its failure is
    // reported via toast.promise's `error` option, not inline.
    const [, options] = mockedToastPromise.mock.calls[0]
    const toastError = options?.error as (err: unknown) => string
    expect(
      toastError(
        new ApiError('internal', 'Couldn’t reach the server. Try again.'),
      ),
    ).toBe('Couldn’t reach the server. Try again.')

    await waitFor(() => expect(screen.getByText('Dining')).toBeInTheDocument())
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a toast (not inline) when reparenting a category fails', async () => {
    const user = userEvent.setup()
    mockedListCategories.mockResolvedValue([
      {
        id: 'cat_food',
        name: 'Food',
        type: 'expense',
        sort_order: 0,
        archived: false,
      },
      {
        id: 'cat_dining',
        name: 'Dining',
        type: 'expense',
        sort_order: 1,
        archived: false,
      },
    ])
    mockedReparentCategory.mockRejectedValue(
      new ApiError('internal', 'Couldn’t reach the server. Try again.'),
    )
    render(<CategoriesSettings />)

    const diningParent = await screen.findByLabelText('Parent', {
      selector: '#parent-cat_dining',
    })
    await user.click(diningParent)
    await user.click(await screen.findByRole('option', { name: 'Food' }))

    // Reparenting a category is a user-triggered action (docs/design-
    // system.md's "Toasts vs. inline messages"), so its failure is
    // reported via a toast, not inline.
    await waitFor(() =>
      expect(mockedToastError).toHaveBeenCalledWith(
        'Couldn’t reach the server. Try again.',
      ),
    )
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
