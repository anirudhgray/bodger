// Playwright config for the web e2e suite (issue #65's smoke test,
// issue #101's dark-mode toggle test). Deliberately one project
// (Chromium only) — docs/architecture.md §7 keeps e2e "deliberately
// sparse", and this suite has no cross-browser matrix to earn. Sparse
// means few, targeted spec files, not necessarily exactly one — a new
// one earns its place for a real user flow or a behaviour Vitest's
// jsdom can't exercise at all, not to click-test every element.
import { defineConfig, devices } from '@playwright/test'

import { E2E_BASE_URL } from './e2e/env'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: 'list',
  globalSetup: './e2e/global-setup.ts',
  timeout: 30_000,
  use: {
    baseURL: E2E_BASE_URL,
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
})
