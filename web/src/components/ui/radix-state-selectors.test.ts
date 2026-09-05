// Guards against the class of bug #101 found in tabs.tsx (fixed there)
// and this pass found again in dialog.tsx, alert-dialog.tsx,
// dropdown-menu.tsx, popover.tsx, sheet.tsx, sidebar.tsx, and
// tooltip.tsx: Radix's open/closed animation state is exposed as
// data-state="open"|"closed" — never as a standalone data-open/
// data-closed attribute. A className like `data-open:animate-in`
// compiles to the attribute selector `[data-open]`, which can never
// match, so the intended animation silently never runs. This is
// deliberately narrow — only `open`/`closed`, not e.g. `data-disabled`
// or `data-inset` (dropdown-menu.tsx's own real boolean attributes,
// correctly bare) or `data-active` (sidebar.tsx sets that one itself,
// also correctly bare — see tabs.test.tsx for the one place `active`
// actually was this bug, on a Radix-driven data-state instead).
import { describe, expect, it } from 'vitest'

const sources = import.meta.glob('./*.tsx', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>

const BROKEN_PATTERN = /data-(?:open|closed):/

describe('Radix open/closed animations use data-[state=...], not a bare data-open/data-closed', () => {
  for (const [path, source] of Object.entries(sources)) {
    it(`${path} doesn't use data-open:/data-closed: (Radix never sets those)`, () => {
      const match = BROKEN_PATTERN.exec(source)
      expect(
        match,
        match
          ? `found "${match[0]}" — Radix sets data-state="open"/"closed", not a standalone boolean attribute; use data-[state=open]:/data-[state=closed]: instead`
          : undefined,
      ).toBeNull()
    })
  }
})
