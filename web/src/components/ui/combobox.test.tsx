// Component tests for Combobox (issue #106, building on #104's Radix
// Popover groundwork): the same real-interaction convention as
// date-picker-impl.test.tsx — cmdk's Command list needs actual pointer-event
// sequences, not plain fireEvent, and jsdom's hasPointerCapture/
// scrollIntoView polyfills (test/setup.ts) are what make that possible
// here without extra per-test setup.
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { Combobox, type ComboboxOption } from './combobox'

const options: ComboboxOption[] = [
  { value: 'food', label: 'Food', depth: 0, path: 'Food' },
  {
    value: 'groceries',
    label: 'Groceries',
    depth: 1,
    path: 'Food > Groceries',
  },
  { value: 'bills', label: 'Bills', depth: 0, path: 'Bills' },
]

describe('Combobox', () => {
  it('controlled: shows the selected option’s full path, opens, and reports a pick via onChange', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(
      <Combobox
        aria-label="Category"
        options={options}
        value="groceries"
        onChange={onChange}
      />,
    )

    expect(
      screen.getByRole('combobox', { name: 'Category' }),
    ).toHaveTextContent('Food > Groceries')

    await user.click(screen.getByRole('combobox', { name: 'Category' }))
    await user.click(await screen.findByRole('option', { name: 'Bills' }))

    expect(onChange).toHaveBeenCalledWith('bills')
  })

  it('controlled: reflects a value update fed back in by the caller', () => {
    const { rerender } = render(
      <Combobox
        aria-label="Category"
        options={options}
        value="food"
        onChange={() => {}}
      />,
    )
    rerender(
      <Combobox
        aria-label="Category"
        options={options}
        value="bills"
        onChange={() => {}}
      />,
    )
    expect(
      screen.getByRole('combobox', { name: 'Category' }),
    ).toHaveTextContent('Bills')
  })

  it('shows the placeholder with no value selected', () => {
    render(
      <Combobox
        aria-label="Category"
        options={options}
        value=""
        onChange={() => {}}
        placeholder="Choose a category"
      />,
    )
    expect(
      screen.getByRole('combobox', { name: 'Category' }),
    ).toHaveTextContent('Choose a category')
  })

  it('uncontrolled: mirrors the picked value into a hidden input under the given name', async () => {
    const user = userEvent.setup()
    const { container } = render(
      <Combobox
        aria-label="Category filter"
        options={options}
        name="category"
      />,
    )

    const hidden = container.querySelector(
      'input[type="hidden"][name="category"]',
    )
    expect(hidden).toHaveValue('')

    await user.click(screen.getByRole('combobox', { name: 'Category filter' }))
    await user.click(await screen.findByRole('option', { name: 'Food' }))

    expect(hidden).toHaveValue('food')
  })

  it('filters the list as you type, matching against the full hierarchy path', async () => {
    const user = userEvent.setup()
    render(
      <Combobox
        aria-label="Category"
        options={options}
        value=""
        onChange={() => {}}
      />,
    )

    await user.click(screen.getByRole('combobox', { name: 'Category' }))
    await user.type(
      await screen.findByRole('combobox', { name: /search/i }),
      'Grocer',
    )

    expect(
      await screen.findByRole('option', { name: 'Groceries' }),
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('option', { name: 'Bills' }),
    ).not.toBeInTheDocument()
  })

  it('offers to create the typed text when nothing matches, and slots the result in as the selection', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    const onCreate = vi
      .fn()
      .mockResolvedValue({ value: 'new-cat', label: 'Subscriptions' })
    render(
      <Combobox
        aria-label="Category"
        options={options}
        value=""
        onChange={onChange}
        onCreate={onCreate}
      />,
    )

    await user.click(screen.getByRole('combobox', { name: 'Category' }))
    await user.type(
      await screen.findByRole('combobox', { name: /search/i }),
      'Subscriptions',
    )

    const createRow = await screen.findByText('Create "Subscriptions"')
    await user.click(createRow)

    expect(onCreate).toHaveBeenCalledWith('Subscriptions')
    expect(onChange).toHaveBeenCalledWith('new-cat')
  })

  it('does not offer to create when the typed text already exactly matches an option', async () => {
    const user = userEvent.setup()
    const onCreate = vi.fn()
    render(
      <Combobox
        aria-label="Category"
        options={options}
        value=""
        onChange={() => {}}
        onCreate={onCreate}
      />,
    )

    await user.click(screen.getByRole('combobox', { name: 'Category' }))
    await user.type(
      await screen.findByRole('combobox', { name: /search/i }),
      'Food',
    )

    expect(screen.queryByText('Create "Food"')).not.toBeInTheDocument()
  })

  it('does not offer to create at all when the caller passes no onCreate', async () => {
    const user = userEvent.setup()
    render(
      <Combobox
        aria-label="Category"
        options={options}
        value=""
        onChange={() => {}}
      />,
    )

    await user.click(screen.getByRole('combobox', { name: 'Category' }))
    await user.type(
      await screen.findByRole('combobox', { name: /search/i }),
      'Something New',
    )

    expect(screen.queryByText('Create "Something New"')).not.toBeInTheDocument()
  })
})
