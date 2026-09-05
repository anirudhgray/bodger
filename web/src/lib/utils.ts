export { cn } from 'cn'

// Sentence-cases a single word for a discrete list of choices — a
// <select> option, a filter, a dropdown item. Some of the same words are
// deliberately lowercase inline in running text elsewhere (e.g.
// TransactionsList's kindLabel, mirroring the CLI's own verb vocabulary
// per docs/ux-principles.md §7) — that's a different context with a
// different casing convention, not a second copy to hand-write. Derive
// one from the other with this rather than writing a new literal string.
export function capitalize(word: string): string {
  return word.length === 0 ? word : word[0].toUpperCase() + word.slice(1)
}

// Strips an amount input down to what it could ever validly become: digits
// and at most one decimal point. The app still sends amount around as a
// raw string end-to-end (never parsed as a JS number — floating point has
// no business anywhere near money) and the server has the last, real word
// on validity (a leading zero, too many decimal places, an empty value) —
// this is just what stops a field literally being able to hold "soemthing"
// in the first place, rather than only rejecting it after a submit
// round-trip. Call this from the input's change handler (a controlled
// input) or its own value on an uncontrolled one — either way, feed it
// back in rather than letting the bad keystroke land.
export function sanitizeAmountInput(value: string): string {
  const digitsAndDots = value.replace(/[^0-9.]/g, '')
  const firstDot = digitsAndDots.indexOf('.')
  if (firstDot === -1) return digitsAndDots
  return (
    digitsAndDots.slice(0, firstDot + 1) +
    digitsAndDots.slice(firstDot + 1).replace(/\./g, '')
  )
}
