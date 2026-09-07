// Shared constants for the web e2e smoke test (issue #65's second bullet).
// Imported by playwright.config.ts (for baseURL), global-setup.ts (to seed
// and start the real bodger binary), and every spec file (to fill in the
// same values) — one source of truth rather than three copies that could
// drift apart. Playwright re-executes this module fresh in each worker
// process it spawns (a different OS process, and a different process.pid,
// than the main process that loaded playwright.config.ts and ran
// global-setup.ts) — see resolvePort's own doc comment for why that
// matters for E2E_PORT specifically.
import path from 'node:path'

// A fixed base port, offset per-run — this suite always runs against its
// own isolated instance, never colliding with a `bodger serve` (or dev
// proxy) a contributor might already have running locally on 8080, and the
// per-run offset keeps two concurrent suite runs (e.g. two parallel
// orchestrate agents in separate git worktrees, per #116) from racing to
// bind the same port.
//
// Can't derive this from process.pid directly: the main process (which
// loads playwright.config.ts and starts the server in global-setup.ts) and
// the worker process(es) actually running the tests (which need the exact
// same port for `page.goto`'s relative URLs to resolve anywhere) are
// different OS processes with different pids — confirmed by instrumenting
// both while fixing #116. Instead the main process picks the port once and
// publishes it via BODGER_E2E_PORT; workers, spawned as its children,
// inherit process.env and read the same value back rather than deriving
// their own.
function resolvePort(): number {
  const inherited = process.env.BODGER_E2E_PORT
  if (inherited) return Number(inherited)
  const port = 47610 + (process.pid % 5000)
  process.env.BODGER_E2E_PORT = String(port)
  return port
}
export const E2E_PORT = resolvePort()
export const E2E_BASE_URL = `http://127.0.0.1:${E2E_PORT}`

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

// bodgerBinName is the platform-appropriate binary filename global-setup.ts
// builds inside its own per-run temp directory — pulled out here only so
// the win32 check isn't duplicated if another caller ever needs it.
export const E2E_BIN_NAME =
  process.platform === 'win32' ? 'bodger-e2e.exe' : 'bodger-e2e'

export function e2eBinPath(e2eDir: string): string {
  return path.join(e2eDir, E2E_BIN_NAME)
}

export function e2eDbPath(e2eDir: string): string {
  return path.join(e2eDir, 'bodger-e2e.db')
}
