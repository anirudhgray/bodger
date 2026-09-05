// index.html carries a small inline script that duplicates resolveTheme's
// resolution rule in plain JS, because it has to run before any ES module
// does (see theme.ts's doc comment). Nothing else keeps that duplicate in
// sync with the real implementation — this extracts the actual script
// text from index.html and runs it against the same cases theme.test.ts
// covers, so a future edit to one without the other fails a test instead
// of silently drifting. web/e2e/theme.spec.ts covers the full page
// round-trip (a real reload, real React, real button) but doesn't
// reliably catch this: useTheme's mount effect re-applies the correct
// class from the real resolveTheme() immediately after mount, which
// masks a wrong initial class from a broken inline script before any
// assertion gets a chance to observe it.
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

// Vite's `?raw` import (declared by the `vite/client` types this project
// already includes) reads the file as a plain string at build/test time —
// keeps this file inside the browser-only tsconfig.app.json project
// rather than needing Node's `fs`/`path`, which aren't typed there.
import indexHtml from '../../index.html?raw'

function extractInlineThemeScript(): string {
  const match = /<script>([\s\S]*?)<\/script>/.exec(indexHtml)
  if (!match) {
    throw new Error(
      "index.html's inline pre-paint <script> is gone or changed shape — " +
        'update this test to match wherever the theme-resolution script ' +
        'moved to.',
    )
  }
  return match[1]
}

function stubPrefersDark(matches: boolean) {
  window.matchMedia = ((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })) as typeof window.matchMedia
}

describe("index.html's inline pre-paint theme script", () => {
  const script = extractInlineThemeScript()
  const run = () => new Function(script)()

  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    document.documentElement.classList.remove('dark')
  })

  it('follows prefers-color-scheme when nothing is stored', () => {
    stubPrefersDark(true)
    run()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('follows prefers-color-scheme (light) when nothing is stored', () => {
    stubPrefersDark(false)
    run()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('prefers a stored light choice over prefers-color-scheme', () => {
    stubPrefersDark(true)
    localStorage.setItem('bodger:theme', 'light')
    run()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('prefers a stored dark choice over prefers-color-scheme', () => {
    stubPrefersDark(false)
    localStorage.setItem('bodger:theme', 'dark')
    run()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('ignores a corrupted stored value rather than throwing', () => {
    stubPrefersDark(false)
    localStorage.setItem('bodger:theme', 'sepia')
    expect(run).not.toThrow()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })
})
