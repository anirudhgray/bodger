import { describe, expect, it } from 'vitest'

import type { Category } from '@/lib/api'
import { buildCategoryTree } from './category-tree'

function cat(
  overrides: Partial<Category> & Pick<Category, 'id' | 'name'>,
): Category {
  return {
    type: 'expense',
    sort_order: 0,
    archived: false,
    ...overrides,
  }
}

describe('buildCategoryTree', () => {
  it('returns a flat list of top-level categories, ordered by sort_order', () => {
    const categories = [
      cat({ id: 'b', name: 'Bills', sort_order: 1 }),
      cat({ id: 'a', name: 'Food', sort_order: 0 }),
    ]

    expect(buildCategoryTree(categories)).toEqual([
      { category: categories[1], depth: 0, path: 'Food' },
      { category: categories[0], depth: 0, path: 'Bills' },
    ])
  })

  it('walks depth-first: a parent is immediately followed by its children', () => {
    const food = cat({ id: 'food', name: 'Food' })
    const groceries = cat({
      id: 'groceries',
      name: 'Groceries',
      parent_id: 'food',
    })
    const dining = cat({ id: 'dining', name: 'Dining', parent_id: 'food' })
    const bills = cat({ id: 'bills', name: 'Bills', sort_order: 1 })

    const tree = buildCategoryTree([bills, dining, groceries, food])

    expect(tree.map((n) => n.category.id)).toEqual([
      'food',
      'dining',
      'groceries',
      'bills',
    ])
    expect(tree.map((n) => n.depth)).toEqual([0, 1, 1, 0])
  })

  it('supports arbitrary depth, not just one level of nesting', () => {
    const food = cat({ id: 'food', name: 'Food' })
    const groceries = cat({
      id: 'groceries',
      name: 'Groceries',
      parent_id: 'food',
    })
    const produce = cat({
      id: 'produce',
      name: 'Produce',
      parent_id: 'groceries',
    })

    const tree = buildCategoryTree([produce, food, groceries])

    expect(
      tree.map((n) => ({ id: n.category.id, depth: n.depth, path: n.path })),
    ).toEqual([
      { id: 'food', depth: 0, path: 'Food' },
      { id: 'groceries', depth: 1, path: 'Food > Groceries' },
      { id: 'produce', depth: 2, path: 'Food > Groceries > Produce' },
    ])
  })

  it('orders siblings by sort_order, falling back to name', () => {
    const food = cat({ id: 'food', name: 'Food' })
    const z = cat({
      id: 'z',
      name: 'Zucchini',
      parent_id: 'food',
      sort_order: 0,
    })
    const a = cat({ id: 'a', name: 'Apples', parent_id: 'food', sort_order: 0 })

    const tree = buildCategoryTree([z, food, a])

    expect(tree.map((n) => n.category.id)).toEqual(['food', 'a', 'z'])
  })

  it('surfaces a category whose parent is missing from the array as top-level, instead of dropping it', () => {
    // e.g. an archived parent filtered out of the list while this
    // still-active (or still-selected) child stays in it.
    const orphan = cat({
      id: 'orphan',
      name: 'Orphan',
      parent_id: 'missing-parent',
    })

    const tree = buildCategoryTree([orphan])

    expect(tree).toEqual([{ category: orphan, depth: 0, path: 'Orphan' }])
  })

  it('guards against a parent_id cycle instead of recursing forever', () => {
    const a = cat({ id: 'a', name: 'A', parent_id: 'b' })
    const b = cat({ id: 'b', name: 'B', parent_id: 'a' })

    const tree = buildCategoryTree([a, b])

    // Neither can be "first" without the other's parent_id already
    // having been walked — both end up surfaced (once each, no
    // duplicates, no infinite loop) via the orphan fallback.
    expect(tree).toHaveLength(2)
    expect(tree.map((n) => n.category.id).sort()).toEqual(['a', 'b'])
  })

  it('returns an empty array for an empty input', () => {
    expect(buildCategoryTree([])).toEqual([])
  })
})
