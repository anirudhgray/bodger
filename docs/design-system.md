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
from `lucide-react`). Add a new primitive with:

```sh
cd web && npx shadcn@latest add <component>
```

Primitives present as of this milestone: `button`, `input`, `label`,
`select` (native, hand-written — see the audit note below), `card`,
`sidebar`, `popover`, `calendar`, `tabs`, `dialog`, `alert-dialog`,
`dropdown-menu`, `tooltip`, `separator`, `skeleton`, `spinner`, `kbd`,
`empty`, `toast` (hand-built against Radix's Toast primitive directly —
see the note below, since it isn't in the shadcn CLI's own registry).

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

`sidebar` is still **not** wired into `AppLayout`'s nav. It's the
right primitive for that chrome eventually, but its collapsible/mobile
behavior (`useIsMobile`, the `Sheet` mobile drawer) *is* issue #88's
scope (responsive layout for small screens) — wiring it now would
preempt that audit rather than support it. Converting the top nav to
`Sidebar` is better done as part of #88, once that issue has actually
looked at the nav at narrow widths.

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
revoke, account/category archive. It's built directly against
`@radix-ui/react-toast` (via the `radix-ui` package, same as every other
primitive here) rather than generated via the shadcn CLI or added as
`sonner` — shadcn's current registry only ships a sonner-based toast,
and adding a second toast/notification library alongside Radix for one
component wasn't worth it. The store (`web/src/hooks/use-toast.ts`) is
the classic pre-sonner shadcn pattern: a module-level listener list plus
a plain `toast()` function, since call sites are as often a plain async
event handler as a component that's already subscribed to anything.

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
don't invent a different visual language component-by-component. The
first cut of the toast primitive violated this (see below); the fix is
the reference going forward.

- **Color is a signal, not a fill.** The toast card itself
  (`components/ui/toast.tsx`) always renders on the neutral
  `bg-popover`/`border-border` surface every other overlay in this app
  uses, regardless of variant — the same "accent is a signal, not a
  fill" principle from this doc's Principles section, applied to a
  primitive that initially got it wrong by tinting the *entire* card
  (`bg-success/10`, full green text) for `success`/`destructive`. The
  variant's color lives on the leading icon (a check, an X-circle, a
  spinner) and the title text only — matching both shadcn's own toast
  examples and this app's existing "Recorded." convention for
  positive-signal text.
- **Primary message goes in `title`, not `description`.** `title` is
  what carries the variant's color; `description` is a secondary,
  always-neutral detail line underneath it, used only when a toast
  needs more than one line. Every current call site is single-line, so
  every current call site passes `title`.
- **`toast.promise(promise, { loading, success, error? })`**
  (`web/src/hooks/use-toast.ts`) — shadcn/sonner's "promise" toast
  pattern, reimplemented on this app's own Radix-based store rather
  than pulling in sonner. Shows a `loading`-variant toast immediately,
  then updates that *same* toast in place to `success` or `destructive`
  once the promise settles (an internal `version` counter forces
  `Toaster` to remount just that toast, so Radix's own auto-dismiss
  timer restarts for the new content instead of inheriting however much
  of the loading toast's — effectively infinite — duration had already
  elapsed). It's a pure side effect: the promise you pass in comes back
  unchanged, so an existing `await`/`try`/`catch` call site needs no
  restructuring.
  - Omit `error` to have the loading toast quietly dismiss on
    rejection instead of showing one — the right choice whenever the
    action's own inline error state (per the bullet above) is already
    going to carry that message, so the two don't say the same thing
    twice. `TransactionsList.tsx`'s `handleDelete` and `Settings.tsx`'s
    `handleRevoke`/`handleArchive` all do this: loading toast for
    the wait, success toast when it lands, inline `error` (not a
    toast) if it doesn't.

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
