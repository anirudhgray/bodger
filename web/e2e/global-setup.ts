// globalSetup for the web e2e smoke test (issue #65's second bullet).
//
// This deliberately does not point Playwright at `npm run dev` — that
// dev server serves over real HTTPS via vite-plugin-mkcert, which
// installs a local CA through an interactive sudo prompt on first run
// (web/vite.config.ts's own comment; issues #86/#85), which is unusable
// in CI or any unattended run. Instead this builds and runs the real
// production artefact: `npm run build` into
// internal/platform/webui/dist, then a real `go build` of the bodger
// binary that embeds it (ADR-0001 — one binary serves the API and the
// static UI, no dev proxy in production) — the same thing `make build`
// produces. That binary is then seeded with a password and one
// account/category via its own CLI (the same one a real user would run
// on first setup — `bodger auth set-password`, issue #76/#57) and
// started listening on a fixed loopback port, isolated from any
// contributor's real bodger.db by BODGER_DB_PATH pointing at a temp file.
//
// Playwright calls the function this returns as teardown once every test
// in the run has finished (https://playwright.dev/docs/test-global-setup-teardown),
// so the server this starts is stopped in the same process that started
// it rather than needing a separate global-teardown file or a PID
// hand-off.
import { execFileSync, spawn, type ChildProcess } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import {
  E2E_ACCOUNT_NAME,
  E2E_BASE_URL,
  E2E_BIN_PATH,
  E2E_CATEGORY_NAME,
  E2E_DB_PATH,
  E2E_OPENING_BALANCE,
  E2E_PASSWORD,
  E2E_PORT,
} from './env'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const WEB_DIR = path.resolve(__dirname, '..')
const REPO_ROOT = path.resolve(WEB_DIR, '..')

const HEALTHY_TIMEOUT_MS = 20_000
const SHUTDOWN_TIMEOUT_MS = 5_000

export default async function globalSetup() {
  const e2eDir = path.dirname(E2E_DB_PATH)
  fs.rmSync(e2eDir, { recursive: true, force: true })
  fs.mkdirSync(e2eDir, { recursive: true })

  console.log('[e2e] building web assets (npm run build)…')
  execFileSync('npm', ['run', 'build'], { cwd: WEB_DIR, stdio: 'inherit' })

  console.log('[e2e] building the bodger binary…')
  execFileSync(
    'go',
    ['build', '-trimpath', '-o', E2E_BIN_PATH, './cmd/bodger'],
    { cwd: REPO_ROOT, stdio: 'inherit' },
  )

  const seedEnv = { ...process.env, BODGER_DB_PATH: E2E_DB_PATH }

  console.log('[e2e] seeding password, account, and category…')
  execFileSync(E2E_BIN_PATH, ['auth', 'set-password'], {
    input: `${E2E_PASSWORD}\n`,
    env: seedEnv,
    stdio: ['pipe', 'inherit', 'inherit'],
  })
  execFileSync(
    E2E_BIN_PATH,
    [
      'accounts',
      'add',
      E2E_ACCOUNT_NAME,
      '--type',
      'bank',
      '--currency',
      'USD',
      '--opening-balance',
      E2E_OPENING_BALANCE,
    ],
    { env: seedEnv, stdio: ['ignore', 'inherit', 'inherit'] },
  )
  execFileSync(
    E2E_BIN_PATH,
    ['categories', 'add', E2E_CATEGORY_NAME, '--type', 'expense'],
    { env: seedEnv, stdio: ['ignore', 'inherit', 'inherit'] },
  )

  console.log(`[e2e] starting bodger serve on ${E2E_BASE_URL}…`)
  const server = spawn(E2E_BIN_PATH, ['serve'], {
    env: {
      ...process.env,
      BODGER_DB_PATH: E2E_DB_PATH,
      BODGER_HTTP_BIND_ADDR: `127.0.0.1:${E2E_PORT}`,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  server.stdout?.on('data', (chunk: Buffer) =>
    process.stdout.write(`[bodger] ${chunk}`),
  )
  server.stderr?.on('data', (chunk: Buffer) =>
    process.stderr.write(`[bodger] ${chunk}`),
  )

  let earlyExit: Error | null = null
  server.once('exit', (code, signal) => {
    if (code !== 0 && code !== null) {
      earlyExit = new Error(
        `bodger serve exited early (code ${code}, signal ${signal})`,
      )
    }
  })

  await waitUntilHealthy(() => earlyExit)

  return async function globalTeardown() {
    await stopServer(server)
  }
}

async function waitUntilHealthy(exited: () => Error | null) {
  const deadline = Date.now() + HEALTHY_TIMEOUT_MS
  while (Date.now() < deadline) {
    const err = exited()
    if (err) throw err
    try {
      const res = await fetch(`${E2E_BASE_URL}/healthz`)
      if (res.ok) return
    } catch {
      // Not listening yet — retry until the deadline.
    }
    await new Promise((resolve) => setTimeout(resolve, 200))
  }
  throw new Error(
    `bodger serve did not become healthy within ${HEALTHY_TIMEOUT_MS}ms at ${E2E_BASE_URL}`,
  )
}

function stopServer(server: ChildProcess): Promise<void> {
  return new Promise((resolve) => {
    if (server.exitCode !== null || server.signalCode !== null) {
      resolve()
      return
    }
    server.once('exit', () => resolve())
    server.kill('SIGTERM')
    setTimeout(() => {
      if (server.exitCode === null && server.signalCode === null) {
        server.kill('SIGKILL')
      }
    }, SHUTDOWN_TIMEOUT_MS)
  })
}
