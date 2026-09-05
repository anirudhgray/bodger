// Covers issue #88's dialog scrollable-content fix (shadcn's own
// documented pattern: https://ui.shadcn.com/docs/components/base/dialog#scrollable-content)
// as a real user flow a real browser viewport is needed for: jsdom has no
// layout engine, so a component test can't tell a genuinely scrollable
// region from one that's just silently clipped or positioned off-screen.
// Before this fix, TransactionDialog's form had no capped, scrollable
// region — DialogContent is vertically centered via `top-1/2
// -translate-y-1/2` with no height limit of its own, so on a short
// viewport, opening "Add details" (which adds four more fields) grows
// the dialog past the viewport symmetrically: confirmed by measurement
// against the pre-fix code, the title ends up entirely above the
// viewport (`getBoundingClientRect().top/bottom` both negative) while
// the rest of the form runs past the bottom. `toBeVisible()` doesn't
// catch this — Playwright's definition is CSS visibility, not viewport
// position — `toBeInViewport()` does.
import { expect, test } from '@playwright/test'

import { E2E_CATEGORY_NAME, E2E_PASSWORD } from './env'

test.use({ viewport: { width: 400, height: 600 } })

test('the transaction dialog stays usable on a short viewport with "Add details" open', async ({
  page,
}) => {
  await page.goto('/login')
  await page.getByLabel('Password').fill(E2E_PASSWORD)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).toHaveURL(/\/transactions$/)

  await page.getByRole('button', { name: 'Record a transaction' }).click()
  const dialog = page.getByRole('dialog', { name: 'Add transaction' })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: 'Add details' }).click()

  const title = dialog.getByRole('heading', { name: 'Add transaction' })
  const submit = dialog.getByRole('button', { name: 'Record spend' })
  await expect(title).toBeInViewport()
  await expect(submit).toBeInViewport()

  // The regression: without a capped, scrollable middle region, the
  // Tags field (added by "Add details") would sit outside the fold with
  // no way to reach it — the dialog itself doesn't scroll, and Radix
  // locks the page body's own scroll while a dialog is open.
  const tags = dialog.getByLabel('Tags')
  await tags.scrollIntoViewIfNeeded()
  await tags.fill('groceries, weekly')
  await expect(tags).toHaveValue('groceries, weekly')

  // Scrolling to reach a lower field must not have pushed the header or
  // the footer's submit button out of view — they stay pinned outside
  // the scroll region, exactly like shadcn's own "sticky footer" example.
  await expect(title).toBeInViewport()
  await expect(submit).toBeInViewport()

  // Confirm the pinned submit button is actually clickable, not just
  // visible-but-covered by something else — without actually submitting,
  // since this suite shares one seeded instance across every spec and a
  // real transaction here would throw off smoke.spec.ts's own balance
  // assertion.
  await dialog.getByLabel('Amount').fill('12.50')
  // The Category field is a Combobox (issue #106), not a native
  // <select> — its popover renders in its own portal, so the option is
  // queried from the whole page rather than scoped to `dialog`.
  await dialog.getByRole('combobox', { name: 'Category' }).click()
  await page.getByRole('option', { name: E2E_CATEGORY_NAME }).click()
  await expect(submit).toBeEnabled()
  // The category combobox is itself a nested Radix dismissable layer
  // (Popover) inside the dialog's own, with its own brief close
  // animation (components/ui/popover.tsx's duration-100) — pressing
  // Escape before that animation finishes and the layer actually
  // unmounts can be consumed by it instead of reaching the dialog, so
  // wait for its content to be gone from the DOM rather than racing it.
  await expect(page.locator('[data-slot="popover-content"]')).toHaveCount(0)

  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
})
