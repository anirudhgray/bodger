// Component tests for the Categories settings subpage (issue #107, split
// out of the old Settings.test.tsx; issue #106 added the tree display
// and swapped the "Parent" field for a Combobox). web/src/lib/settings
// is mocked here so these exercise only the page's own logic against a
// controlled fake API. The Parent field is a real interaction target
// now, not a native <select> — see combobox.test.tsx and
// TransactionDialog.test.tsx's pickCategory for the same convention.
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, it, expect, vi } from 'vitest'

vi.mock('@/lib/settings', () => ({
  listCategories: vi.fn(),
  createCategory: vi.fn(),
  renameCategory: vi.fn(),
  archiveCategory: vi.fn(),
  reparentCategory: vi.fn(),
}))

import { listCategories, reparentCategory } from '@/lib/settings'
import { CategoriesSettings } from './Categories'

const mockedListCategories = vi.mocked(listCategories)
const mockedReparentCategory = vi.mocked(reparentCategory)

describe('CategoriesSettings', () => {
  beforeEach(() => {
    mockedListCategories.mockReset().mockResolvedValue([])
    mockedReparentCategory.mockReset()
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
})
