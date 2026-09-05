// Covers issue #101's dark-mode toggle as a real user flow through the
// whole stack: log in, flip the toggle, reload, confirm the choice
// actually survives — the same round trip a real visitor depends on,
// against a real build of the production binary. This is not what
// catches index.html's inline pre-paint script drifting from
// src/lib/theme.ts's resolveTheme: useTheme's mount effect re-applies
// the correct class from the real resolveTheme() immediately after
// mount, which would mask a broken inline script before this test's
// assertions get a chance to observe it. See
// src/lib/inline-theme-script.test.ts for the test that actually
// isolates and guards that duplication.
import { expect, test } from '@playwright/test'

import { E2E_PASSWORD } from './env'

test('dark mode toggle flips the theme and persists across reload', async ({
  page,
}) => {
  await page.goto('/login')
  await page.getByLabel('Password').fill(E2E_PASSWORD)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).toHaveURL(/\/transactions$/)

  const html = page.locator('html')
  await expect(html).not.toHaveClass(/dark/)

  await page.getByRole('button', { name: 'Switch to dark mode' }).click()
  await expect(html).toHaveClass(/dark/)

  // The class survives a reload only if index.html's inline script reads
  // localStorage correctly, before React (and lib/theme.ts) ever mounts.
  await page.reload()
  await expect(html).toHaveClass(/dark/)
  await expect(
    page.getByRole('button', { name: 'Switch to light mode' }),
  ).toBeVisible()

  await page.getByRole('button', { name: 'Switch to light mode' }).click()
  await expect(html).not.toHaveClass(/dark/)

  await page.reload()
  await expect(html).not.toHaveClass(/dark/)
})
