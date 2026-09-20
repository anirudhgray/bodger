// The import-review occurrence-match e2e flow (issue #307's own coverage
// requirement, and the #309/#312 web-UI gap this PR closes alongside it):
// stage a CSV row that deterministically matches a pending recurring
// occurrence, accept that match through the review screen's own Decision
// column, and see it become a real transaction — the one part of #307
// with a genuine write effect a script can exercise without a live
// typesafe.ai key. This exercises resolveImportRecordOccurrenceMatch end
// to end against the real REST surface, not a mock of it (Import.test.tsx
// already covers this screen's own orchestration in isolation).
//
// #307's own AI-suggestion *display* layer (the "Accept match" card
// attributed to typesafe.ai) is deliberately NOT covered here: this suite
// runs with no BODGER_TYPESAFE_API_KEY set (global-setup.ts never sets
// one — no key, no credits spent), so GET .../suggestions genuinely
// returns { configured: false }. This spec's own assertion of the
// resulting "aren't set up on this instance" line below is that
// not-configured state exercised for real, not mocked. The present/failed
// suggestion states can't be produced without a live key or a network
// mock this project has no manual-QA fixture for (see this PR's own
// report) — they're covered by Import.test.tsx's mocked-API vitest
// coverage instead.
import { expect, test } from '@playwright/test'

import { E2E_CATEGORY_NAME, E2E_PASSWORD } from './env'

// A dedicated account and its own recurring rule, created fresh by this
// spec — same reasoning as recurring.spec.ts's own
// E2E_RECURRING_ACCOUNT_NAME: materialising an occurrence below really
// does move money (data-model.md §11), and playwright.config.ts runs
// every spec against one shared server/DB (fullyParallel: false), so
// posting against the shared "Checking" account would change its balance
// out from under smoke.spec.ts's own opening-balance arithmetic.
const E2E_IMPORT_ACCOUNT_NAME = 'E2E Import Suggestions'
const E2E_RULE_DESCRIPTION = 'E2E import rent'
// Deliberately not recurring.spec.ts's own "50.00": both specs' final
// assertions run against the shared /transactions list in the same
// server/DB (playwright.config.ts's single worker), so a same-amount
// outflow from each spec would leave a page-wide amount query ambiguous
// between the two.
const E2E_RULE_AMOUNT = '63.10'

// Computed once here, at spec-load time, the same way recurring.spec.ts's
// own todayISO/todayDayOfMonth are — a recurring rule's first occurrence
// landing "today" depends on whatever day the suite actually runs.
const today = new Date()
const todayISO = today.toISOString().slice(0, 10)
const todayDayOfMonth = String(today.getDate())

test('accept a deterministic occurrence match on a staged import row, materialising it into a real transaction', async ({
  page,
}) => {
  await page.goto('/login')
  await page.getByLabel('Password').fill(E2E_PASSWORD)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).toHaveURL(/\/transactions$/)

  // A dedicated account for this spec's own real transaction.
  await page.goto('/settings/accounts')
  await page.getByLabel('New account name').fill(E2E_IMPORT_ACCOUNT_NAME)
  // Scoped to .last(): the sidebar's own global "Add" button (opens the
  // transaction dialog) also matches this role+name.
  await page.getByRole('button', { name: 'Add' }).last().click()
  await expect(page.getByText(E2E_IMPORT_ACCOUNT_NAME)).toBeVisible()

  // A monthly rule whose day-of-month is today's own, so its first
  // projected occurrence lands today — refreshing right after creating it
  // is guaranteed to produce exactly one pending occurrence to match a
  // staged row against, regardless of which day the suite runs on.
  await page.goto('/recurring')
  await page.getByRole('button', { name: 'New rule' }).click()
  const ruleDialog = page.getByRole('dialog', { name: 'New recurring rule' })
  await expect(ruleDialog).toBeVisible()
  await ruleDialog.getByLabel('Description').fill(E2E_RULE_DESCRIPTION)
  await ruleDialog.getByRole('combobox', { name: 'Account' }).click()
  await page.getByRole('option', { name: E2E_IMPORT_ACCOUNT_NAME }).click()
  await ruleDialog.getByRole('combobox', { name: 'Category' }).click()
  await page.getByRole('option', { name: E2E_CATEGORY_NAME }).click()
  await ruleDialog.getByLabel('Amount').fill(E2E_RULE_AMOUNT)
  await ruleDialog.getByLabel('Day').fill(todayDayOfMonth)
  await ruleDialog.getByRole('button', { name: 'Create rule' }).click()
  await expect(ruleDialog).toBeHidden()

  await page.getByRole('button', { name: 'Refresh occurrences' }).click()
  await expect(page.locator('li', { hasText: todayISO })).toBeVisible()

  // Stage a CSV row for the same account, date, and (Groceries is an
  // expense category) signed amount, with a description token-identical
  // to the rule's own — internal/app/import_duplicate.go's
  // findOccurrenceMatch's own deterministic eligibility: same account,
  // exact signed amount and currency, occurrence_date within the
  // duplicate-detection date window, and a similar description. None of
  // that is a model judgement (ADR-0015) — it's exactly what this spec
  // sets up by hand.
  await page.goto('/import-export/import')
  await page.getByRole('button', { name: 'New import' }).click()
  await page.getByRole('combobox', { name: 'Account' }).click()
  await page.getByRole('option', { name: E2E_IMPORT_ACCOUNT_NAME }).click()
  await page.getByLabel('File').setInputFiles({
    name: 'statement.csv',
    mimeType: 'text/csv',
    buffer: Buffer.from(
      `Date,Description,Amount\n${todayISO},${E2E_RULE_DESCRIPTION},-${E2E_RULE_AMOUNT}\n`,
    ),
  })
  await page.getByRole('button', { name: 'Continue' }).click()
  await page.getByRole('button', { name: 'Stage for review' }).click()

  // No typesafe.ai key is set for this suite — the review screen's own
  // quiet, non-blocking not-configured line, exercised for real.
  await expect(
    page.getByText('AI-assisted suggestions aren’t set up on this instance.'),
  ).toBeVisible()

  const row = page.getByRole('row', { name: E2E_RULE_DESCRIPTION })
  await expect(row).toBeVisible()
  // The deterministic gap-closing badge (issue #309/#312's own web UI,
  // never shipped until this PR) — independent of, and a prerequisite
  // for, any AI suggestion.
  await expect(row.getByText('Matches a pending occurrence')).toBeVisible()

  const commitButton = page.getByRole('button', { name: 'Commit import' })
  await expect(commitButton).toBeDisabled()

  await row.getByRole('button', { name: 'Materialise' }).click()
  await expect(row.getByText('Occurrence materialised')).toBeVisible()
  await expect(commitButton).toBeEnabled()

  await commitButton.click()
  await expect(page.getByRole('button', { name: 'Commit import' })).toBeHidden()

  // The occurrence became a real transaction; the staged row that matched
  // it was excluded from commit rather than creating a second one for the
  // same money (internal/domain/importing/record.go's SettleStatus).
  await page.getByRole('link', { name: 'Transactions' }).click()
  await expect(page).toHaveURL(/\/transactions$/)
  await expect(page.getByText(E2E_RULE_DESCRIPTION)).toBeVisible()
  await expect(
    page.getByText(new RegExp(`${E2E_RULE_AMOUNT} USD`)),
  ).toBeVisible()

  // Archive this spec's own rule once done with it, for the same reason
  // as the amount above — one shared server/DB across the whole suite,
  // so a rule left active here would otherwise clutter the Recurring
  // page for any spec that runs after this one. Scoped by its own
  // Archive button, not just its description text: GenerateOccurrences
  // projects this same description onto many months' worth of upcoming
  // occurrence rows too, which a bare hasText match would also catch.
  await page.goto('/recurring')
  const ruleRow = page
    .locator('li')
    .filter({ has: page.getByRole('button', { name: 'Archive' }) })
    .filter({ hasText: E2E_RULE_DESCRIPTION })
  await ruleRow.getByRole('button', { name: 'Archive' }).click()
  await expect(ruleRow).toBeHidden()
})
