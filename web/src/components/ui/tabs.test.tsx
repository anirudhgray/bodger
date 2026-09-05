// Regression test for a real bug found during manual review of #101:
// TabsTrigger's active-state classes were written as `data-active:...`,
// but Radix's TabsTrigger sets `data-state="active"|"inactive"` — it
// never sets a standalone `data-active` attribute at all. Tailwind's
// `data-active:` variant matches the literal `[data-active]` attribute
// selector, which never existed here, so the active tab silently never
// got its distinguishing background/shadow — no visual indication of
// which tab was selected. The fix targets `data-[state=active]:`
// instead; this test guards against the wrong prefix coming back.
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { Tabs, TabsList, TabsTrigger } from './tabs'

describe('TabsTrigger', () => {
  it('keys its active styling off the data-state Radix actually sets', () => {
    render(
      <Tabs defaultValue="a">
        <TabsList>
          <TabsTrigger value="a">A</TabsTrigger>
          <TabsTrigger value="b">B</TabsTrigger>
        </TabsList>
      </Tabs>,
    )

    const active = screen.getByRole('tab', { name: 'A' })
    const inactive = screen.getByRole('tab', { name: 'B' })

    expect(active).toHaveAttribute('data-state', 'active')
    expect(inactive).toHaveAttribute('data-state', 'inactive')
    // Confirms the premise: Radix never sets this, so a selector keyed
    // off it (the actual bug) can never match.
    expect(active).not.toHaveAttribute('data-active')

    expect(active.className).toContain('data-[state=active]:bg-background')
    expect(active.className).not.toContain('data-active:')
  })
})
