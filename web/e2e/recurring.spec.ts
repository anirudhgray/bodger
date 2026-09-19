// The recurring-transactions e2e flow (issue #282's own coverage
// requirement): create a rule, generate (refresh) its occurrences, see a
// projected one, materialise it, and see it become a real transaction.
// Mirrors smoke.spec.ts's own shape and scope — a real build of the
// production binary (global-setup.ts), proving the pieces integrate, not
// a substitute for this screen's own Vitest component tests
// (Recurring.test.tsx, Analytics.test.tsx), which already cover its
// rendering/orchestration with the API mocked.
import { expect, test } from '@playwright/test'

import { E2E_CATEGORY_NAME, E2E_PASSWORD } from './env'

// A dedicated account, created fresh by this spec rather than reusing
// global-setup.ts's shared E2E_ACCOUNT_NAME ("Checking") — playwright.config.ts
// runs every spec file against the same server/DB in one worker
// (fullyParallel: false), and materialising an occurrence below really
// does move money (data-model.md §11: that's the whole point of
// materialising). Posting it against the shared account would change its
// balance out from under smoke.spec.ts's own opening-balance arithmetic,
// which assumes it's the only thing touching that account. A fresh
// account here keeps this spec's real transaction fully isolated.
const E2E_RECURRING_ACCOUNT_NAME = 'E2E Recurring'

// todayISO/todayDayOfMonth are computed once here, at spec-load time, in
// the same local wall-clock the browser's own `new Date()` (and, in
// practice, the bodger server it talks to) resolves "today" from — never
// asserted against a literal date the way smoke.spec.ts's opening-balance
// arithmetic is, since a recurring rule's "first occurrence lands today"
// depends on whatever day the suite actually runs.
const today = new Date()
const todayISO = today.toISOString().slice(0, 10)
const todayDayOfMonth = String(today.getDate())

test('create a recurring rule, generate its occurrences, materialise one into a real transaction', async ({
  page,
}) => {
  await page.goto('/login')
  await page.getByLabel('Password').fill(E2E_PASSWORD)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).toHaveURL(/\/transactions$/)

  await page.goto('/settings/accounts')
  await page.getByLabel('New account name').fill(E2E_RECURRING_ACCOUNT_NAME)
  // Scoped to .last(): the sidebar's own global "Add" button (opens the
  // transaction dialog) also matches this role+name; the account form's
  // own submit button is the second match in DOM order.
  await page.getByRole('button', { name: 'Add' }).last().click()
  await expect(page.getByText(E2E_RECURRING_ACCOUNT_NAME)).toBeVisible()

  await page.getByRole('link', { name: 'Recurring' }).click()
  await expect(page).toHaveURL(/\/recurring$/)
  await expect(page.getByRole('heading', { name: 'Recurring' })).toBeVisible()

  // Create a monthly rule whose day-of-month is today's own — its first
  // projected occurrence (starts_on defaults to today, left unset here)
  // lands on today, so refreshing right after creating it is guaranteed
  // to produce at least one occurrence to materialise, regardless of
  // which day of the month the suite happens to run on.
  await page.getByRole('button', { name: 'New rule' }).click()
  const dialog = page.getByRole('dialog', { name: 'New recurring rule' })
  await expect(dialog).toBeVisible()

  await dialog.getByLabel('Description').fill('E2E rent')
  await dialog.getByRole('combobox', { name: 'Account' }).click()
  await page.getByRole('option', { name: E2E_RECURRING_ACCOUNT_NAME }).click()
  await dialog.getByRole('combobox', { name: 'Category' }).click()
  await page.getByRole('option', { name: E2E_CATEGORY_NAME }).click()
  await dialog.getByLabel('Amount').fill('50.00')
  await dialog.getByLabel('Day').fill(todayDayOfMonth)
  await dialog.getByRole('button', { name: 'Create rule' }).click()
  await expect(dialog).toBeHidden()

  // The rule now shows in its own list — a real, still-not-money-moved
  // template (data-model.md §11), distinct from the occurrences section
  // below.
  await expect(page.getByText('E2E rent')).toBeVisible()
  await expect(page.getByText(/−50\.00 USD/)).toBeVisible()

  // Nothing is projected until refresh is explicitly pressed (ADR-0014:
  // generation is never automatic) — there is no occurrence to
  // materialise yet.
  await expect(page.getByText('Upcoming (projected)')).toBeVisible()
  await expect(
    page.getByText(
      'Nothing pending. Use “Refresh occurrences” above to generate upcoming dates from your active rules.',
    ),
  ).toBeVisible()

  await page.getByRole('button', { name: 'Refresh occurrences' }).click()

  // The pending occurrence for today: visually distinguished with its own
  // "Projected" badge, inside the separate, dashed/muted "Upcoming
  // (projected)" card — data-model.md §11's structural-and-visual
  // distinction from an actual transaction.
  const occurrenceRow = page.locator('li', { hasText: todayISO })
  await expect(occurrenceRow).toBeVisible()
  await expect(occurrenceRow.getByText('Projected')).toBeVisible()

  await occurrenceRow.getByRole('button', { name: 'Materialise' }).click()
  // Materialising resolves that one row — either it disappears (no more
  // pending occurrences dated today) or, if the rule projected more than
  // one occurrence on today's date across the generation horizon (it
  // never does for a monthly schedule), at least one fewer remains. The
  // simplest, always-true assertion here is the real payoff: the
  // transaction it produced is now visible on /transactions.
  await expect(occurrenceRow).toBeHidden()

  await page.getByRole('link', { name: 'Transactions' }).click()
  await expect(page).toHaveURL(/\/transactions$/)
  await expect(page.getByText('E2E rent')).toBeVisible()
  await expect(page.getByText(/50\.00 USD/)).toBeVisible()
})
