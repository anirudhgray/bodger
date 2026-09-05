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

// jsdom doesn't implement window.matchMedia at all (not even a stub that
// always reports no match), so anything that calls it — lib/theme.ts's
// prefersDark, for the dark-mode toggle (issue #101) — throws in every
// test unless something provides one first. This always reports "no
// match": tests that care about a specific prefers-color-scheme value
// stub it themselves (see hooks/use-theme.test.ts), and everything else
// just needs the call not to throw.
if (typeof window.matchMedia !== 'function') {
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })
}
