// Covers issue #88's sidebar wiring as a real user flow a real browser
// viewport is needed for: on a narrow viewport AppLayout's nav renders as
// an off-canvas sheet (web/src/components/ui/sidebar.tsx's Sheet-backed
// mobile branch, driven by web/src/hooks/use-mobile.ts's real
// matchMedia), which jsdom's component tests can't exercise the way a
// real browser does. This pins down a real bug found while building this
// feature: following a nav link inside the sheet didn't close it, leaving
// the sheet's overlay on top of the very page it just navigated to and
// blocking every control underneath — App.tsx's closeMobileSidebar fixes
// it by calling setOpenMobile(false) on click.
import { expect, test } from '@playwright/test'

import { E2E_PASSWORD } from './env'

test.use({ viewport: { width: 375, height: 700 } })

test('the mobile sidebar closes after following a nav link, leaving the destination page usable', async ({
  page,
}) => {
  await page.goto('/login')
  await page.getByLabel('Password').fill(E2E_PASSWORD)
  await page.getByRole('button', { name: 'Log in' }).click()
  await expect(page).toHaveURL(/\/transactions$/)

  await page.getByRole('button', { name: 'Toggle Sidebar' }).click()
  const sidebarNav = page.getByRole('dialog')
  await expect(sidebarNav.getByRole('link', { name: 'Balances' })).toBeVisible()

  await sidebarNav.getByRole('link', { name: 'Balances' }).click()
  await expect(page).toHaveURL(/\/balances$/)

  // The regression: a still-open sheet overlay intercepts pointer events
  // on whatever's underneath it, so this click would time out if
  // App.tsx's onClick handler stopped closing the sidebar on navigate.
  await expect(sidebarNav).toBeHidden()
  await page.getByRole('button', { name: 'Toggle Sidebar' }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
})
