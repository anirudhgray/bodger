# Design system

The web UI's visual language: color, type, spacing, radius, elevation, and
the component primitives built on them. Established for
[M3 · Rivendell](architecture.md#8-milestones) (issue #100), replacing the
all-neutral shadcn install defaults `web/`'s scaffold (#69) shipped with.

This document covers *look*. For *behavior and words* — vocabulary,
fast-entry rules, progressive disclosure, error phrasing — see
[`ux-principles.md`](ux-principles.md), which every surface (not just the
web UI) is held to.

---

## Principles

- **Neutrals are neutral.** The gray scale carries a near-imperceptible
  cool tint (hue 240 in OKLCH) and is not warmed or colored toward the
  brand accent. Two independent hues, not one hue at different
  saturations.
- **The accent is a signal, not a fill.** A muted, deep green (hue 155),
  reserved for one primary action per screen, focus rings, links, and
  small positive-signal text (a "Recorded." confirmation). It does not
  fill nav items, tab pills, or secondary chrome — those stay neutral.
  This was a deliberate correction: an earlier draft used the accent too
  freely and read as playful rather than as a finance tool.
- **Every color is OKLCH**, chosen so accents share lightness and chroma
  and vary only in hue — swapping the accent hue later (a full rebrand)
  is a one-line change, not a redesign.
- **Cards get real elevation.** A hairline border plus a subtle
  `shadow-sm` for anything that groups content (a card, a form panel,
  a grouped list) — flat-bordered-only reads as unfinished once other
  surfaces (menus, popovers) already use shadow for the same job.

## Color tokens

Each token is a CSS custom property in `web/src/index.css`, mapped to a
Tailwind color utility (`bg-primary`, `text-muted-foreground`, …) via the
`@theme inline` block. Values below are light mode; `.dark` overrides every
one.

| Token | Light | Usage |
| --- | --- | --- |
| `background` / `foreground` | `oklch(0.99 0.002 240)` / `oklch(0.17 0.006 240)` | Page canvas and default text |
| `card` / `card-foreground` | `oklch(1 0.001 240)` | Cards, the transaction-entry panel |
| `popover` / `popover-foreground` | same as `card` | Menus, popovers, the date picker |
| `primary` / `primary-foreground` | `oklch(0.32 0.05 155)` | The one CTA per screen ("Record spend") |
| `secondary` / `secondary-foreground` | `oklch(0.95 0.004 240)` | Low-emphasis buttons, the active-nav-item background |
| `muted` / `muted-foreground` | `oklch(0.96 0.004 240)` / `oklch(0.5 0.008 240)` | Placeholder text, disabled state, subtle backgrounds |
| `accent` / `accent-foreground` | `oklch(0.96 0.004 240)` | Generic hover background (menu items, etc.) — **not** the brand accent; an unfortunate shadcn naming collision, kept for compatibility with generated primitives |
| `success` / `success-foreground` | `oklch(0.42 0.1 155)` | Positive confirmation text only ("Recorded.") |
| `destructive` | `oklch(0.577 0.245 27.325)` | Errors, destructive actions — unchanged from the original scaffold |
| `border` / `input` | `oklch(0.9 0.006 240)` | Hairlines, input borders |
| `ring` | `oklch(0.32 0.05 155)` | Focus ring — equals `primary` |
| `sidebar`, `sidebar-foreground`, `sidebar-accent`, `sidebar-accent-foreground`, `sidebar-border`, `sidebar-ring` | see `index.css` | The sidebar's own slightly-distinct surface, per shadcn's `sidebar.tsx` |

## Type scale

Geist Variable (`@fontsource-variable/geist`), weights 400/500/600.

| Token | Size / line-height | Weight | Used for |
| --- | --- | --- | --- |
| `text-xs` | 12 / 16 | 400 | Helper text |
| `text-sm` | 14 / 20 | 400 | Body, labels |
| `text-sm` medium | 14 / 20 | 500 | List rows, nav items |
| `text-base` | 16 / 24 | 400 | Form controls |
| `text-xl` | 20 / 28 | 600, tracking -0.01em | Section headings |
| `text-2xl` | 24 / 32 | 600, tracking -0.01em | Page headings, amounts |

## Spacing

Tailwind's default 4px scale, unchanged — this names the steps the app
already uses rather than introducing a new one: `4px` icon-to-label,
`8px` label-to-control, `16px` form-field gaps, `24px` section padding,
`32px`/`48px` page gutters (mobile/desktop).

## Radius

Unchanged from the original scaffold (`--radius: 0.625rem`, i.e. 10px):
`radius-sm` 6px, `radius-md` 8px, `radius-lg` 10px, `radius-xl` 14px.

## Elevation

| Level | CSS | Usage |
| --- | --- | --- |
| Flat | none | Nav, table rows, in-page dividers |
| `shadow-xs` | `0 1px 2px 0 rgb(0 0 0 / 0.05)` | Inputs, buttons |
| `shadow-sm` (card) | `0 1px 3px 0 rgb(0 0 0 / .06), 0 1px 2px -1px rgb(0 0 0 / .06)` | Cards, grouped lists, the entry form panel |
| `shadow-sm` (overlay) | `0 1px 3px 0 rgb(0 0 0 / .1), 0 1px 2px -1px rgb(0 0 0 / .1)` | Menus, popovers |

## Dark mode

`.dark` carries a full set of overrides for every token above. A real
toggle (issue #101), not just `prefers-color-scheme`, lives in
`AppLayout`'s nav (`web/src/App.tsx`): `web/src/lib/theme.ts` resolves
and persists the choice (`localStorage`, key `bodger:theme` — the stored
value always wins once set), `web/src/hooks/use-theme.ts` wires that into
React, and `index.html` carries a small inline script that applies the
same resolution before first paint, so there's no flash of the wrong
theme while the bundle loads. `web/src/lib/inline-theme-script.test.ts`
extracts and runs that actual script text against the same cases
`theme.test.ts` covers, so the two drifting apart fails a test instead of
silently shipping (`web/e2e/theme.spec.ts` covers the toggle as a real
user flow across a reload, but React's own mount effect masks a broken
inline script from that test — see its doc comment). Keep the script's
resolution rule in sync with `lib/theme.ts`'s `resolveTheme` if it ever
changes — it's duplicated
in plain JS deliberately, since it has to run before any module does.

## Component primitives

`web/src/components/ui/` is managed by the [shadcn CLI](https://ui.shadcn.com)
(`web/components.json`, style `radix-nova`, base color `neutral`, icons
from `lucide-react`). This is a hard requirement, not a suggestion: add a
new primitive, or refresh an existing one against the registry, with the
CLI —

```sh
cd web && npx shadcn@latest add <component>          # new primitive
cd web && npx shadcn@latest add <component> --overwrite  # refresh an existing one
```

— never by hand-writing or hand-editing a primitive's own file to match
what shadcn's site shows. `--overwrite` replaces the file outright, which
also **discards any local fix layered onto that primitive** (PR #105's
button `cursor-pointer` fix was lost this way while investigating #88's
sidebar work, and had to be caught by diffing before committing) — always
run `git diff` on the result and confirm nothing project-specific
disappeared before keeping it, and prefer fixing a real bug via a
`@custom-variant`/token in `index.css` (see the gotcha below) over an
`--overwrite` when the two would conflict.

**Gotcha: Tailwind's bare `data-{name}:` shorthand is presence-only, not
value-exact.** It compiles `data-active:` to plain `[data-active]`, so it
matches an element whether the value is `"true"` or `"false"` — and React
renders a boolean `data-*` prop as the literal string `"false"` rather
than omitting the attribute, so it's *always* present. `sidebar.tsx`'s
`data-active:*` (the current-nav-item style) hits this directly: every
item ends up "active" without an exact-value override. The same shorthand
form also appears in the *upstream* registry's current `sheet.tsx` as
`data-open:`/`data-closed:`, which never matches anything at all (Radix
sets `data-state="open"|"closed"`, not a literal `data-open` attribute) —
confirmed by pulling it via `--overwrite`, which silently dropped the
sidebar drawer's slide/fade entrance animation. `index.css` registers
`@custom-variant data-active`/`data-open`/`data-closed` against the exact
Radix/prop value instead, so a primitive's classes stay an unmodified
shadcn pull and a future re-pull doesn't reintroduce either bug.

Primitives present as of this milestone: `button`, `input`, `label`,
`select` (native, hand-written — see the audit note below), `card`,
`sidebar`, `popover`, `calendar`, `tabs`, `dialog`, `alert-dialog`,
`dropdown-menu`, `tooltip`, `separator`, `skeleton`, `spinner`, `kbd`,
`empty`, `sonner` (shadcn's current toast component — see the note
below for why this isn't a hand-built Radix Toast).

**`cn`** (`web/src/lib/utils.ts`) re-exports the
[`cn` npm package](https://github.com/shadcn-ui/cn) — shadcn's own
compiled clsx+tailwind-merge replacement, which its newer generated
components (`button.tsx`, `card.tsx`, …) import directly. The
hand-written primitives (`label.tsx`) import it via `@/lib/utils` instead;
both resolve to the same function, so there's one merge behavior in the
codebase, not two.

**Wiring status, after #101's audit.** `card` now houses every grouped
list and callout that used to be a hand-rolled `border rounded-lg`
`<div>`/`<ul>` (Balances' account list, the transaction list, and
Settings' token/account/category lists and "token created" callout) —
`card.tsx` itself picked up the `shadow-sm` elevation this doc already
called for but the primitive was missing. `tabs` replaced
TransactionEntry's hand-rolled Spend/Receive/Move switch (a `Button` row
faking `role="radiogroup"`) with a real `TabsList`/`TabsTrigger`, which
is the honest semantics for three mutually-exclusive views. `select`
(the native one) now backs every `<select>` on `TransactionsList` and
`Settings` that used to hand-roll the same `border-input bg-background
...` className inline. `empty` and `spinner` replaced every page's own
hand-rolled `<p>Loading…</p>` and `<p>No … yet.</p>` text (Balances,
Settings' token list, TransactionsList) with the same two primitives
everywhere a screen has nothing to show yet.

`sidebar` (issue #88) now backs `AppLayout`'s nav, replacing the old
`<header>`+`<nav>` bar: a static sidebar at `md` and up, an off-canvas
`Sheet` drawer below it (`useIsMobile`'s 768px breakpoint), toggled by
the same `SidebarTrigger` in both cases. Each nav item carries the icon
its own screen already uses for its empty state (`Wallet` for Balances,
`Receipt` for Transactions — reusing established vocabulary rather than
picking new icons for the same concept) and an `aria-current="page"`
alongside the visual active-state highlight. A mobile nav-link click
closes the drawer via `setOpenMobile(false)` before navigating — without
it the sheet's overlay is left covering the destination page, pinned
down by `web/e2e/mobile-nav.spec.ts`.

`select` swapped for the Radix-based version, plus `popover`+`calendar`,
`dialog`/`alert-dialog`, and `dropdown-menu`, are still not wired into
any page: they're all Radix-portal-based and need
`@testing-library/user-event` plus jsdom polyfills (`hasPointerCapture`,
`scrollIntoView`) this project doesn't have yet — landing an untested
interaction swap isn't worth the risk. That remaining wiring is tracked
in [#104](https://github.com/anirudhgray/bodger/issues/104), scoped
separately from #101 so it isn't silently dropped. `kbd` has no call
site yet — there's no keyboard-shortcut UI in the app to hang it on.

`toast` (issue #108) is wired into every action whose own UI (a dialog,
a row) closes or moves on before its result would otherwise be shown:
TransactionDialog's create/edit success, transaction delete, API token
revoke, account/category archive. It's built on **sonner**
(`components/ui/sonner.tsx`, shadcn's current toast component), not a
hand-built wrapper around `@radix-ui/react-toast` the way every other
primitive here is — the Radix Toast primitive itself is deprecated
upstream in favor of sonner, and a first pass that hand-built one
directly against Radix Toast was replaced once that became clear. Call
sites `import { toast } from 'sonner'` directly (`toast.success(...)`,
`toast.promise(...)`) rather than going through an app-owned wrapper
function — sonner's own API already covers what a wrapper would have
added (stacking, per-toast auto-dismiss, a loading→success/error
promise pattern), so there's nothing left for one to do.

## Toasts vs. inline messages

Two different places a success/error message can live, and both are
correct for their own case:

- **Inline, persistent** — form validation tied to a specific field or
  action whose UI is still on screen: a rejected submit still showing
  its own form, an invalid amount, a load error blocking a whole page.
  `docs/ux-principles.md` §6 already governs this content; a toast would
  disappear and leave the user without the field- or action-level
  context of *what* failed. TransactionDialog's `submitError`,
  Settings' per-section `error`, and TransactionsList's `error` all stay
  inline for exactly this reason.
- **Toast** — an action's own triggering UI (a dialog, a row) has
  already closed or is about to, and the result has nowhere left to
  land: TransactionDialog's create/edit success (the dialog closes
  immediately), transaction delete, API token revoke, account/category
  archive success (the row's own controls are what triggered the
  action, and stay on screen, but there was previously no feedback that
  anything happened at all — and for delete/revoke/archive specifically,
  no inline "in progress" affordance either, which is what
  `toast.promise` below is for).

Don't wire a toast into a form validation error — those stay inline —
and don't retrofit every existing inline error into a toast; most are
correctly inline already. This cuts both ways for a single action, not
just success vs. failure in general: delete/revoke/archive show a toast
on *success* (the row's about to disappear, or already has) but stay
inline on *failure* (the row stays put, so the existing per-section
`error` state is still the one place that message belongs — showing it
in both places at once would just be the same sentence twice).

## Toast styling and the loading→success/error pattern

General rule for every primitive in this app, toast included: start
from shadcn's own default styling and interaction conventions, and
apply only this project's specific theme tokens and quirks on top —
don't invent a different visual language component-by-component. A
first cut of this primitive violated that rule twice over: it was
hand-built directly against `@radix-ui/react-toast` (deprecated
upstream in favor of sonner) *and* tinted the entire card
(`bg-success/10`, full green text) for `success`/`destructive` instead
of following shadcn/sonner's own neutral-card-plus-icon look. Both are
fixed by using **sonner** (`components/ui/sonner.tsx`) as-is rather
than reimplementing its behavior:

- **`components/ui/sonner.tsx`** wraps sonner's own `<Toaster/>`,
  themed via CSS variables (`--normal-bg`, `--success-text`,
  `--error-text`, etc., set from this app's own `--popover`/`--success`/
  `--destructive` tokens) rather than custom variant classes — sonner's
  built-in success/error/loading icons and card styling do the rest, so
  there's no hand-written `cva` variant map to keep in sync. `theme` is
  read from this app's own `useTheme()` (`hooks/use-theme.tsx`), not
  `next-themes` (shadcn's reference implementation assumes `next-themes`
  since its docs are Next-first; this app has its own theme context, so
  the wrapper reads from that instead — same idea, different source).
- Call sites `import { toast } from 'sonner'` directly and call
  `toast.success(message)` / `toast.promise(promise, opts)` — there's no
  app-owned `toast()` wrapper function to import instead. `title` vs.
  `description` doesn't apply here: sonner's `toast.success(message)`
  takes the message directly as its first argument.
- **`toast.promise(promise, { loading, success, error? })`** is sonner's
  own API, not a reimplementation — it shows a loading toast immediately
  and updates it to success/error once `promise` settles, with sonner's
  own default icons for each state.
  - **Gotcha:** sonner's `toast.promise` returns a toast id, not the
    promise you passed in (unlike a naive wrapper might) — so it can't
    be `await`ed for its own resolution/rejection. Every call site that
    still needs the actual result (to update state on success, or
    `catch` to set an inline error) keeps its own reference to the
    promise and awaits that separately, passing the same reference to
    `toast.promise` purely for the toast's side effect:
    ```ts
    const action = doTheThing()
    toast.promise(action, { loading: '…', success: 'Done.' })
    try {
      await action
      // update state on success
    } catch (err) {
      setError(...) // the real error handling
    }
    ```
    `TransactionsList.tsx`'s `handleDelete` and `Settings.tsx`'s
    `handleRevoke`/`handleArchive` all follow this shape.
  - Omit `error` to have the loading toast quietly resolve away on
    rejection instead of showing one — the right choice whenever the
    action's own inline error state (per the "Toasts vs. inline
    messages" section above) is already going to carry that message,
    so the two don't say the same thing twice. All three call sites
    above do this.

## Casing enum values for display

The same underlying value can read differently depending on where it
appears, and both are correct for their own context — but each context's
render must come from the *same* underlying label, not a second
hand-written copy:

- **A discrete list of choices** — a `<select>` option, a filter, a
  dropdown item — reads as Title Case: "Bank", "Credit card", "Spend".
- **Inline in a sentence** may deliberately stay lowercase to read
  naturally, or follow a different surface's own vocabulary on purpose
  (`docs/ux-principles.md` §7) — TransactionsList's `kindLabel` mirrors
  the CLI's own verb ("spend", not "Spend") for text like "spend $42.50
  from Checking".

When both are needed for the same value, derive one from the other —
`web/src/lib/utils.ts`'s `capitalize()` for the simple "just capitalize
the first letter" case (`TransactionsList`'s `KIND_OPTIONS`, built from
`kindLabel` rather than a third hand-written lowercase string) — rather
than writing a new literal string that can silently drift from the
others. A value that needs real word substitution, not just
capitalization (`credit_card` → "Credit card"), still gets its own
mapping function (`accountKindLabel`, `categoryKindLabel` in
`Settings.tsx`), but that function is the *only* place that value's
display text is written.

## Adding a color

Never invent a color outside this system. A new accent hue: same
chroma and lightness as the existing one, only the hue changes. A new
neutral: same near-zero chroma, same hue (240) as the rest of the gray
scale. If neither fits, that's a sign the need is a new *semantic*
token (like `success` was), not a one-off hex value.
