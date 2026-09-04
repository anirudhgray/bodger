// Shared constants for the web e2e smoke test (issue #65's second bullet).
// Imported by playwright.config.ts (for baseURL), global-setup.ts (to seed
// and start the real bodger binary), and smoke.spec.ts (to fill in the
// same values) — one source of truth rather than three copies that could
// drift apart.
import os from 'node:os'
import path from 'node:path'

// A fixed, unusual port rather than bodger serve's own 8080 default — this
// suite always runs against its own isolated instance, never colliding
// with a `bodger serve` (or dev proxy) a contributor might already have
// running locally on 8080.
export const E2E_PORT = 47610
export const E2E_BASE_URL = `http://127.0.0.1:${E2E_PORT}`

// An isolated temp directory, not the repo's own working directory or a
// contributor's real bodger.db (CLAUDE.md: tests must not touch real
// data). global-setup.ts removes any leftovers here before each run, so a
// prior crashed run never leaks state into this one.
const E2E_DIR = path.join(os.tmpdir(), 'bodger-e2e')
export const E2E_DB_PATH = path.join(E2E_DIR, 'bodger-e2e.db')
export const E2E_BIN_PATH = path.join(
  E2E_DIR,
  process.platform === 'win32' ? 'bodger-e2e.exe' : 'bodger-e2e',
)

export const E2E_PASSWORD = 'e2e-smoke-test-password'
export const E2E_ACCOUNT_NAME = 'Checking'
export const E2E_CATEGORY_NAME = 'Groceries'
export const E2E_OPENING_BALANCE = '100.00'
export const E2E_SPEND_AMOUNT = '25.00'
// 100.00 opening balance minus a 25.00 spend, computed once here rather
// than by the test doing its own arithmetic — the test asserts against
// this fixed literal instead of re-deriving what the app should already
// get right (that arithmetic is proven elsewhere; see this suite's own
// doc comment in smoke.spec.ts).
export const E2E_EXPECTED_BALANCE = '75.00'
