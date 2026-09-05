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
theme while the bundle loads. Keep that script's resolution rule in sync
with `lib/theme.ts`'s `resolveTheme` if it ever changes — it's duplicated
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
`empty`.

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
...` className inline.

`sidebar` is still **not** wired into `AppLayout`'s nav. It's the
right primitive for that chrome eventually, but its collapsible/mobile
behavior (`useIsMobile`, the `Sheet` mobile drawer) *is* issue #88's
scope (responsive layout for small screens) — wiring it now would
preempt that audit rather than support it. Converting the top nav to
`Sidebar` is better done as part of #88, once that issue has actually
looked at the nav at narrow widths.

`select` swapped for the Radix-based version, plus `popover`+`calendar`
and `dialog`/`alert-dialog`, are still not wired into any page: they're
all Radix-portal-based and need `@testing-library/user-event` plus jsdom
polyfills (`hasPointerCapture`, `scrollIntoView`) this project doesn't
have yet — landing an untested interaction swap isn't worth the risk.
That remaining wiring is tracked in
[#104](https://github.com/anirudhgray/bodger/issues/104), scoped
separately from #101 so it isn't silently dropped.

## Adding a color

Never invent a color outside this system. A new accent hue: same
chroma and lightness as the existing one, only the hue changes. A new
neutral: same near-zero chroma, same hue (240) as the rest of the gray
scale. If neither fits, that's a sign the need is a new *semantic*
token (like `success` was), not a one-off hex value.
