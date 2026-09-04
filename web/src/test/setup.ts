// Vitest setup (web/vite.config.ts's test.setupFiles): registers
// @testing-library/jest-dom's matchers (toBeInTheDocument, etc.) on
// Vitest's own `expect`, run once before every test file.
import '@testing-library/jest-dom/vitest'

// @testing-library/react normally registers its own afterEach(cleanup)
// automatically, but only if it finds a global `afterEach` — which
// requires vitest's `test.globals: true` (vite.config.ts deliberately
// doesn't set that, to keep test-only APIs out of every file's ambient
// scope). Without this, a test file that calls render() more than once
// leaves every earlier render's DOM sitting in document.body, so a later
// query can match stale elements from an earlier test as well as the
// current one (issue #59: this is what broke a getByRole('button', {name:
// 'Log in'}) query in Login.test.tsx). Registered once, centrally, here.
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

afterEach(() => {
  cleanup()
})
