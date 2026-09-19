import { describe, expect, it } from 'vitest'

import type { Schedule } from '@/lib/api'
import { scheduleSummary } from './schedule'

function schedule(
  overrides: Partial<Schedule> & Pick<Schedule, 'frequency'>,
): Schedule {
  return { interval: 1, ...overrides }
}

describe('scheduleSummary', () => {
  it('renders a weekly schedule with its weekday', () => {
    expect(scheduleSummary(schedule({ frequency: 'weekly', weekday: 1 }))).toBe(
      'Every week on Monday',
    )
  })

  it('pluralizes an interval greater than one', () => {
    expect(
      scheduleSummary(
        schedule({ frequency: 'weekly', interval: 2, weekday: 0 }),
      ),
    ).toBe('Every 2 weeks on Sunday')
  })

  it('renders a monthly schedule with an ordinal day', () => {
    expect(
      scheduleSummary(schedule({ frequency: 'monthly', day_of_month: 5 })),
    ).toBe('Every month on the 5th')
    expect(
      scheduleSummary(schedule({ frequency: 'monthly', day_of_month: 31 })),
    ).toBe('Every month on the 31st')
    expect(
      scheduleSummary(schedule({ frequency: 'monthly', day_of_month: 11 })),
    ).toBe('Every month on the 11th')
  })

  it('renders a yearly schedule with its month and day', () => {
    expect(
      scheduleSummary(
        schedule({ frequency: 'yearly', month: 12, day_of_month: 25 }),
      ),
    ).toBe('Every year on December 25')
  })

  it('renders an interval-2 yearly schedule', () => {
    expect(
      scheduleSummary(
        schedule({
          frequency: 'yearly',
          interval: 2,
          month: 1,
          day_of_month: 15,
        }),
      ),
    ).toBe('Every 2 years on January 15')
  })
})
