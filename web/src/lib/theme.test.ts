// Unit tests for the dark-mode toggle's resolution rule (issue #101):
// a stored preference always wins; absent one, prefers-color-scheme
// decides. index.html's inline script duplicates this same rule in
// plain JS (it has to run before any module does) — these tests cover
// the one real implementation the rest of the app actually imports.
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { applyTheme, persistTheme, resolveTheme, storedTheme } from './theme'

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

describe('resolveTheme', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  afterEach(() => {
    document.documentElement.classList.remove('dark')
  })

  it('follows prefers-color-scheme when nothing is stored', () => {
    stubPrefersDark(true)
    expect(resolveTheme()).toBe('dark')

    stubPrefersDark(false)
    expect(resolveTheme()).toBe('light')
  })

  it('prefers a stored choice over prefers-color-scheme', () => {
    stubPrefersDark(true)
    persistTheme('light')

    expect(resolveTheme()).toBe('light')
    expect(storedTheme()).toBe('light')
  })

  it('ignores a corrupted stored value rather than throwing', () => {
    localStorage.setItem('bodger:theme', 'sepia')
    stubPrefersDark(false)

    expect(storedTheme()).toBeNull()
    expect(resolveTheme()).toBe('light')
  })
})

describe('applyTheme', () => {
  afterEach(() => {
    document.documentElement.classList.remove('dark')
  })

  it('toggles the dark class on the document root', () => {
    applyTheme('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)

    applyTheme('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })
})
