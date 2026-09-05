// The golden-path e2e test issue #65 asks for: log in, record a
// transaction, see the updated balance, log out — a smoke test proving
// the pieces integrate through a real build of the production binary
// (global-setup.ts), not a substitute for each screen's own Vitest
// component tests (web/src/pages/*.test.tsx), which already cover this
// screen's behaviour in isolation with the API mocked. Per
// docs/architecture.md §7, e2e stays deliberately sparse — financial
// correctness itself is established at the domain/app layer
// (internal/domain, internal/app), not here. See theme.spec.ts for the
// suite's other test, which exists for a different reason (a behaviour
// jsdom can't exercise at all, not another user flow).
import { expect, test } from '@playwright/test'

import {
  E2E_ACCOUNT_NAME,
  E2E_CATEGORY_NAME,
  E2E_EXPECTED_BALANCE,
  E2E_PASSWORD,
  E2E_SPEND_AMOUNT,
} from './env'

test('log in, record a transaction, see the updated balance, log out', async ({
  page,
}) => {
  await page.goto('/login')
  await page.getByLabel('Password').fill(E2E_PASSWORD)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).toHaveURL(/\/transactions$/)

  await page.getByRole('button', { name: 'Add' }).click()
  const dialog = page.getByRole('dialog', { name: 'Add transaction' })
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('Amount').fill(E2E_SPEND_AMOUNT)
  await dialog.getByLabel('Category').selectOption({ label: E2E_CATEGORY_NAME })
  await dialog.getByRole('button', { name: 'Record spend' }).click()
  await expect(dialog).toBeHidden()
  await expect(page).toHaveURL(/\/transactions$/)

  await page.getByRole('link', { name: 'Balances' }).click()
  await expect(page).toHaveURL(/\/balances$/)
  await expect(page.getByText(E2E_ACCOUNT_NAME)).toBeVisible()
  await expect(page.getByText(`${E2E_EXPECTED_BALANCE} USD`)).toBeVisible()

  await page.getByRole('button', { name: 'Log out' }).click()
  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole('heading', { name: 'Log in' })).toBeVisible()
})
