// Component tests for the Categories settings subpage (issue #107, split
// out of the old Settings.test.tsx). web/src/lib/settings is mocked here
// so these exercise only the page's own logic against a controlled fake
// API.
import { render, screen } from '@testing-library/react'
import { beforeEach, describe, it, expect, vi } from 'vitest'

vi.mock('@/lib/settings', () => ({
  listCategories: vi.fn(),
  createCategory: vi.fn(),
  renameCategory: vi.fn(),
  archiveCategory: vi.fn(),
  reparentCategory: vi.fn(),
}))

import { listCategories } from '@/lib/settings'
import { CategoriesSettings } from './Categories'

const mockedListCategories = vi.mocked(listCategories)

describe('CategoriesSettings', () => {
  beforeEach(() => {
    mockedListCategories.mockReset().mockResolvedValue([])
  })

  it('only offers same-kind categories as a Parent option', async () => {
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

    const parentSelect = await screen.findByLabelText('Parent', {
      selector: '#parent-cat_dining',
    })
    const optionLabels = Array.from(
      parentSelect.querySelectorAll('option'),
    ).map((o) => o.textContent)
    // Dining (expense) may become top-level or sit under Groceries
    // (expense), but never under Salary (income) — one typed tree,
    // data-model.md §6.
    expect(optionLabels).toEqual(['Top level', 'Groceries'])
  })
})
