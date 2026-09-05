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
import { afterEach, vi } from 'vitest'
import { cleanup } from '@testing-library/react'

afterEach(() => {
  cleanup()
})

// jsdom doesn't implement window.matchMedia at all (not even a stub that
// always reports no match), so anything that calls it — lib/theme.ts's
// prefersDark, for the dark-mode toggle (issue #101) — throws in every
// test unless something provides one first. This always reports "no
// match": tests that care about a specific prefers-color-scheme value
// stub it themselves (see lib/theme.test.ts), and everything else just
// needs the call not to throw.
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

// jsdom doesn't implement the Clipboard API at all — navigator.clipboard
// is undefined, so anything that calls writeText (components/ui's
// CopyButton) throws in every test unless something provides a stub
// first. A plain vi.fn() lets tests assert on what was copied; cleared
// (not just reset — a test can still supply its own mockResolvedValue if
// it needs one) after every test alongside the cleanup() above, so one
// test's calls never leak into the next in the same file.
if (typeof navigator.clipboard === 'undefined') {
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: vi.fn() },
    configurable: true,
  })
}

afterEach(() => {
  // @testing-library/user-event's setup() (needed for Radix
  // popover/select interaction, see the polyfills below) unconditionally
  // installs its own clipboard stub over navigator.clipboard the first
  // time it runs, replacing this file's vi.fn()-based one — so
  // writeText is no longer a mock in any test file that calls
  // userEvent.setup(), even once. Guard rather than assume.
  if (vi.isMockFunction(navigator.clipboard?.writeText)) {
    vi.mocked(navigator.clipboard.writeText).mockClear()
  }
})

// jsdom doesn't implement ResizeObserver at all — Radix's Switch (used by
// components/ui/switch.tsx, e.g. TransactionDialog's "Enter multiple"
// toggle) measures its thumb with it internally, and throws on mount in
// every test without a stub. A no-op is enough: nothing here asserts on
// layout, only on state (checked/unchecked).
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

// jsdom doesn't implement Element.prototype.hasPointerCapture or
// Element.prototype.scrollIntoView at all — Radix's Popover/Select
// machinery (components/ui/popover.tsx, calendar.tsx's day-picker
// buttons) calls both during pointer/keyboard interaction, and throws
// without a stub. Neither test here asserts on capture state or scroll
// position, so a no-op is enough.
if (typeof Element.prototype.hasPointerCapture !== 'function') {
  Element.prototype.hasPointerCapture = () => false
}
if (typeof Element.prototype.scrollIntoView !== 'function') {
  Element.prototype.scrollIntoView = () => {}
}
