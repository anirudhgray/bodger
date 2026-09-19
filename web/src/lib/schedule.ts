// scheduleSummary renders a recurring rule's Schedule (internal/domain/
// recurring's RFC 5545 subset — weekly/monthly/yearly, ADR-0014) as a
// short, human sentence for the Recurring screen (issue #282). Purely a
// text rendering of fields the server already validated and returned —
// never a schedule computation of its own (no next-occurrence-date maths
// here; that's GenerateOccurrences' job server-side).
import type { Schedule } from './api'

const WEEKDAY_LABELS = [
  'Sunday',
  'Monday',
  'Tuesday',
  'Wednesday',
  'Thursday',
  'Friday',
  'Saturday',
]

const MONTH_LABELS = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
]

// ordinal renders 1 -> "1st", 2 -> "2nd", 3 -> "3rd", 4 -> "4th", 11-13 ->
// "11th"/"12th"/"13th" (the standard English exceptions), etc.
function ordinal(n: number): string {
  const mod100 = n % 100
  if (mod100 >= 11 && mod100 <= 13) return `${n}th`
  switch (n % 10) {
    case 1:
      return `${n}st`
    case 2:
      return `${n}nd`
    case 3:
      return `${n}rd`
    default:
      return `${n}th`
  }
}

function every(interval: number, unit: string): string {
  return interval === 1 ? `Every ${unit}` : `Every ${interval} ${unit}s`
}

export function scheduleSummary(schedule: Schedule): string {
  switch (schedule.frequency) {
    case 'weekly': {
      const weekday =
        schedule.weekday === null || schedule.weekday === undefined
          ? null
          : WEEKDAY_LABELS[schedule.weekday]
      const base = every(schedule.interval, 'week')
      return weekday ? `${base} on ${weekday}` : base
    }
    case 'monthly': {
      const base = every(schedule.interval, 'month')
      return schedule.day_of_month
        ? `${base} on the ${ordinal(schedule.day_of_month)}`
        : base
    }
    case 'yearly': {
      const base = every(schedule.interval, 'year')
      const month =
        schedule.month === null || schedule.month === undefined
          ? null
          : MONTH_LABELS[schedule.month - 1]
      if (month && schedule.day_of_month) {
        return `${base} on ${month} ${schedule.day_of_month}`
      }
      return base
    }
  }
}
