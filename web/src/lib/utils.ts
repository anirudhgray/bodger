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
