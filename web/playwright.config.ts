// Playwright config for issue #65's web e2e smoke test. Deliberately one
// project (Chromium only) and one spec file — docs/architecture.md §7
// keeps e2e "deliberately sparse", and a single golden-path smoke test
// has no cross-browser matrix to earn.
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
