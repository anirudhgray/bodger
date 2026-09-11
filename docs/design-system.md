# Design system

The web UI's visual language: color, type, spacing, radius, elevation, and
the component primitives built on them. Established for
[M3 · Rivendell](architecture.md#8-milestones) (issue #100), replacing the
all-neutral shadcn install defaults `web/`'s scaffold (#69) shipped with.

This document covers _look_. For _behavior and words_ — vocabulary,
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

| Token                                                                                                            | Light                                             | Usage                                                                                                                                                            |
| ---------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `background` / `foreground`                                                                                      | `oklch(0.99 0.002 240)` / `oklch(0.17 0.006 240)` | Page canvas and default text                                                                                                                                     |
| `card` / `card-foreground`                                                                                       | `oklch(1 0.001 240)`                              | Cards, the transaction-entry panel                                                                                                                               |
| `popover` / `popover-foreground`                                                                                 | same as `card`                                    | Menus, popovers, the date picker                                                                                                                                 |
| `primary` / `primary-foreground`                                                                                 | `oklch(0.32 0.05 155)`                            | The one CTA per screen ("Record spend")                                                                                                                          |
| `secondary` / `secondary-foreground`                                                                             | `oklch(0.95 0.004 240)`                           | Low-emphasis buttons, the active-nav-item background                                                                                                             |
| `muted` / `muted-foreground`                                                                                     | `oklch(0.96 0.004 240)` / `oklch(0.5 0.008 240)`  | Placeholder text, disabled state, subtle backgrounds                                                                                                             |
| `accent` / `accent-foreground`                                                                                   | `oklch(0.96 0.004 240)`                           | Generic hover background (menu items, etc.) — **not** the brand accent; an unfortunate shadcn naming collision, kept for compatibility with generated primitives |
| `success` / `success-foreground`                                                                                 | `oklch(0.42 0.1 155)`                             | Positive confirmation text only ("Recorded.")                                                                                                                    |
| `destructive`                                                                                                    | `oklch(0.577 0.245 27.325)`                       | Errors, destructive actions — unchanged from the original scaffold                                                                                               |
| `border` / `input`                                                                                               | `oklch(0.9 0.006 240)`                            | Hairlines, input borders                                                                                                                                         |
| `ring`                                                                                                           | `oklch(0.32 0.05 155)`                            | Focus ring — equals `primary`                                                                                                                                    |
| `sidebar`, `sidebar-foreground`, `sidebar-accent`, `sidebar-accent-foreground`, `sidebar-border`, `sidebar-ring` | see `index.css`                                   | The sidebar's own slightly-distinct surface, per shadcn's `sidebar.tsx`                                                                                          |

## Type scale

Geist Variable (`@fontsource-variable/geist`), weights 400/500/600.

| Token            | Size / line-height | Weight                | Used for               |
| ---------------- | ------------------ | --------------------- | ---------------------- |
| `text-xs`        | 12 / 16            | 400                   | Helper text            |
| `text-sm`        | 14 / 20            | 400                   | Body, labels           |
| `text-sm` medium | 14 / 20            | 500                   | List rows, nav items   |
| `text-base`      | 16 / 24            | 400                   | Form controls          |
| `text-xl`        | 20 / 28            | 600, tracking -0.01em | Section headings       |
| `text-2xl`       | 24 / 32            | 600, tracking -0.01em | Page headings, amounts |

## Spacing

Tailwind's default 4px scale, unchanged — this names the steps the app
already uses rather than introducing a new one: `4px` icon-to-label,
`8px` label-to-control, `16px` form-field gaps, `24px` section padding,
`32px`/`48px` page gutters (mobile/desktop).

## Radius

Unchanged from the original scaffold (`--radius: 0.625rem`, i.e. 10px):
`radius-sm` 6px, `radius-md` 8px, `radius-lg` 10px, `radius-xl` 14px.

## Elevation

| Level                 | CSS                                                             | Usage                                      |
| --------------------- | --------------------------------------------------------------- | ------------------------------------------ |
| Flat                  | none                                                            | Nav, table rows, in-page dividers          |
| `shadow-xs`           | `0 1px 2px 0 rgb(0 0 0 / 0.05)`                                 | Inputs, buttons                            |
| `shadow-sm` (card)    | `0 1px 3px 0 rgb(0 0 0 / .06), 0 1px 2px -1px rgb(0 0 0 / .06)` | Cards, grouped lists, the entry form panel |
| `shadow-sm` (overlay) | `0 1px 3px 0 rgb(0 0 0 / .1), 0 1px 2px -1px rgb(0 0 0 / .1)`   | Menus, popovers                            |

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
than omitting the attribute, so it's _always_ present. `sidebar.tsx`'s
`data-active:*` (the current-nav-item style) hits this directly: every
item ends up "active" without an exact-value override. The same shorthand
form also appears in the _upstream_ registry's current `sheet.tsx` as
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
`collapsible`, `checkbox` (issue #139 — see "Balances: rate-provenance
detail row" below), `empty`, `sonner` (shadcn's current toast component
— see the note below for why this isn't a hand-built Radix Toast),
`command` and `combobox` (issue #106 — see the "Combobox and category
hierarchy" section below; the first primitives here not built on
Radix).

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

`popover`+`calendar` landed as a real date picker in #118
(`components/ui/date-picker.tsx`, wired into TransactionDialog's date
field and TransactionsList's from/to filters). The native
`<select>`-to-Radix swap landed in two steps: #106/#104 first replaced
it with a Combobox (below) for exactly the three category pickers that
needed hierarchy display and search; a follow-up pass then rebuilt
`components/ui/select.tsx` itself on Radix (`SelectTrigger`/
`SelectContent`/`SelectItem`, not a Combobox) and moved every _other_
`<select>` in the app onto it — see "Select: Radix, not native" below
for why the plain native trigger wasn't good enough on its own.
`dialog`/`alert-dialog` and `dropdown-menu` are still not wired into
any page: #104's own text frames both as "if a real spot turns up,"
and none has — Settings' rows still just have plain Rename/Archive/
Revoke buttons, and deletes/archives are deliberately unconfirmed
(`docs/ux-principles.md` §5). `kbd` has no call site yet — there's no
keyboard-shortcut UI in the app to hang it on.

**Select: Radix, not native.** `components/ui/select.tsx` was
originally a plain native `<select>` styled to match `Input` — fine for
its closed trigger, but its _open_ dropdown was unstyled native
OS/browser chrome, the one form control left that didn't respect the
app's own light/dark theme. It's now a themed Radix `Select`
(`SelectTrigger`/`SelectContent`/`SelectItem`/`SelectValue`, following
this project's usual thin-wrapper conventions), used for every flat,
non-hierarchical, non-searchable list in the app: `settings/Accounts.tsx`'s
account-type dropdown, `settings/Categories.tsx`'s own category-type
(Expense/Income) dropdown, TransactionDialog's account and "to account"
fields, and TransactionsList's filter panel's account and type fields.
`combobox.tsx` stays reserved for pickers that need search, hierarchy,
or quick-create — none of these do, so the lighter primitive (no `cmdk`,
no search box) is the boring, proportional choice for them.

A real Radix Select gotcha surfaced building this: its trigger renders
a hidden native `<select>` "bubble" mirror whenever it sits inside an
actual `<form>` (true for every field above, regardless of whether a
`name` prop is passed for `FormData` participation), keyed to the set
of option values. Mounting a `Select` before its options are ready and
letting them arrive afterward (TransactionDialog's account fields start
with zero accounts until `listAccounts()` resolves) changes that key
mid-lifecycle, which forces Radix to destroy and recreate the mirror
and, in the process, dispatch a synthetic `change` event that silently
resets the controlled value back to `""` — confirmed against
`@radix-ui/react-select`'s own `SelectBubbleInput` source, not a
jsdom-only artifact. `TransactionDialog.tsx`'s account fields render a
"Loading accounts…" placeholder until the real list has arrived, rather
than mounting the `Select` early with an empty option set; the "to
account" field additionally carries `key={accountId}` to force a clean
remount (a fresh, complete option list from the start) whenever the
excluded "from" account changes, since that same option-set change can
otherwise happen again mid-session, not just on first load.

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

`tabs` (issue #107) also now backs Settings' own sub-navigation
(`pages/settings/SettingsLayout.tsx`), split into `/settings/password`,
`/tokens`, `/accounts`, and `/categories` routes rather than one long
stacked page. This is a second, different use of the same primitive from
TransactionEntry's Spend/Receive/Move switch above: there, `Tabs` swaps
content in place within one page; here, each `TabsTrigger` is a real
`<Link>` (via `asChild`) to its own route, so the "tabs" are actual
navigation — the address bar, back button, and bookmarks all work
normally.

The same four routes are also reachable straight from `AppLayout`'s own
top-level nav (#88): `AppSidebar`'s Settings entry is a `Collapsible` +
`SidebarMenuSub` group rather than a plain link, collapsed by default
and opened automatically when a settings route is already active. This
isn't a second, competing nav structure — `Sidebar` and the content-area
tab strip serve different reach: the tab strip is for someone already on
a settings page switching sections, the sidebar submenu is for jumping
straight to one (e.g. "Categories") from anywhere else in the app
without a stop at `/settings/password` first. `SidebarMenuSub`'s
trigger only expands/collapses; it doesn't navigate on its own, so
reaching a section always means picking one from the list, the same as
the tab strip does.

## Toasts vs. inline messages

Two different places a success/error message can live, and which one
applies is decided by _what kind of thing failed_, not by whether the
triggering UI happens to still be on screen:

- **Inline, persistent — load failures.** Fetching data to populate a
  page or a section on mount (`listAccounts()`, `listCategories()`,
  `listTransactions()`, `getBalances()`, `getReportingCurrency()`'s
  initial fetch, and the equivalent in every settings subpage) shows its
  failure inline, next to the content that failed to load: `role="alert"`,
  persistent text, still there until the next successful load replaces
  it. `docs/ux-principles.md` §6 already governs this content — a toast
  would disappear and leave the user staring at a blank or stale section
  with no explanation still on screen. TransactionDialog's `loadError`,
  every Settings subpage's own list-load error, TransactionsList's
  `error` (its `listTransactions()`/`listAccounts()`/`listCategories()`
  path specifically), and Balances'/Currency's own initial-fetch `error`
  are all this case.
- **Toast — action failures.** The result of a user-triggered
  submit/mutation — recording or editing a transaction, deleting one,
  creating/renaming/archiving/reparenting an account or category,
  setting the reporting currency, creating or revoking an API token —
  reports failure via `toast.error(message)`, the same place its
  success already goes. There's no inline error rendered alongside it;
  the toast is the sole failure feedback for these. This applies
  whether or not the triggering UI is still on screen: TransactionDialog's
  `submitError` (the dialog stays open on failure) and TransactionsList's
  `handleDelete` (the row stays put on failure) are both actions, so
  both are toasts now, same as create/edit success and
  delete/revoke/archive success already were.
- **Toast + inline, persistent — credential actions.** Logging in and
  changing the password are actions too, so both still toast on
  failure — but paired with a persistent inline alert as well
  (`Login.tsx`, `settings/Password.tsx`), unlike every other action
  above. A toast a user is slow to notice, or steps away from, leaves no
  trace once it auto-dismisses; for an ordinary CRUD action that's fine
  because the surrounding UI (a row still present, a dialog still open)
  already carries the context of what was being attempted, but a bare
  login/password screen has no such standing state to fall back on —
  losing the message here means losing the only explanation for why
  nothing happened.

Don't show both for an ordinary action failure — the toast replaces the
inline message there, it doesn't sit alongside it. Login and password
change are the deliberate exception (above), not a precedent to extend
case-by-case; a new action needing this treatment should have as
concrete a justification as "there's no other visible state that
explains the failure." A page or component can still mix load-inline
and action-toast: TransactionsList's own `error` state is a load
failure (stays inline) while its `handleDelete` is an action (toast) —
same screen, two different `error`-shaped things for two different
reasons, not one state reused for both. This also cuts both ways for a
single action's own success vs. failure, the same as before:
delete/revoke/archive show a toast either way now, so `toast.promise`'s
`error` option (below) is no longer omitted for these — see the next
section.

## Toast styling and the loading→success/error pattern

General rule for every primitive in this app, toast included: start
from shadcn's own default styling and interaction conventions, and
apply only this project's specific theme tokens and quirks on top —
don't invent a different visual language component-by-component. A
first cut of this primitive violated that rule twice over: it was
hand-built directly against `@radix-ui/react-toast` (deprecated
upstream in favor of sonner) _and_ tinted the entire card
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
    still needs the actual result (to update state on success, or to
    know when the mutation has settled) keeps its own reference to the
    promise and awaits that separately, passing the same reference to
    `toast.promise` purely for the toast's side effect:
    ```ts
    const action = doTheThing();
    toast.promise(action, {
      loading: "…",
      success: "Done.",
      error: (err) => errorMessage(err),
    });
    try {
      await action;
      // update state on success
    } catch {
      // failure is already reported via the `error` option above
    }
    ```
    `TransactionsList.tsx`'s `handleDelete` and the Settings
    subpages' (`Accounts.tsx`, `ApiTokens.tsx`, `Categories.tsx`)
    `handleRevoke`/`handleArchive` all follow this shape.
  - Give `error` a real value (a string, or a function of the
    rejection) rather than omitting it — every action failure reports
    through the toast now (per the "Toasts vs. inline messages" section
    above), so there's no inline state left for an omitted `error` to
    avoid duplicating.

## Combobox and category hierarchy

Issue #106 needed a picker that could show real indentation and host an
inline "create new" row — neither a native `<option>` list nor a grouped
Radix `Select` (one level of nesting via `SelectGroup`/`SelectLabel`) can
do both, and categories nest to arbitrary depth in the domain model
(`internal/domain/ledger/category.go`'s `ParentID`, no cap on how deep a
chain can go), so a grouped `Select` would only ever handle one level.
The fit is shadcn's Combobox pattern: Radix `Popover` + `Command`
(search-as-you-type, arbitrary-depth indentation, and a natural place for
a create row), which meant adding this project's first non-Radix
primitive:

- **`components/ui/command.tsx`** wraps [cmdk](https://cmdk.paco.me/),
  themed with this app's own tokens the same way every other primitive
  here wraps its own Radix package — `Command`, `CommandInput`,
  `CommandList`, `CommandEmpty`, `CommandGroup`, `CommandItem`,
  `CommandSeparator`. cmdk isn't Radix, but it's the closest thing to a
  "primitive" this interaction needs, and it composes with the existing
  `popover.tsx` the same way `calendar.tsx` already does.
- **`components/ui/combobox.tsx`**'s `Combobox` composes `command.tsx`
  and `popover.tsx` into one widget, generic over a plain
  `ComboboxOption` (`value`, `label`, optional `depth` and `path`) —
  nothing in it is category-specific. It follows `date-picker.tsx`'s
  controlled/uncontrolled convention exactly: pass `value`+`onChange`
  for a `useState`-backed field (TransactionDialog's Category), or
  `name`+`defaultValue` for an uncontrolled `<form>`+`FormData` field
  (TransactionsList's filter panel) — a hidden input mirrors the picked
  value in the latter case, the same trick `date-picker.tsx` uses.
- **`lib/category-tree.ts`**'s `buildCategoryTree` turns the flat
  `Category[]` every endpoint returns into a depth-first, depth- and
  path-annotated walk — written once and reused by all three call
  sites below, rather than three copies of the same parent_id-chasing
  logic. `depth` drives the open list's indentation (`0.75rem` per
  level in the combobox, `20px` per level in Settings' own list —
  different scales for a compact popover row vs. a full list row);
  `path` (the full "Parent > Child" ancestry) is what a collapsed
  trigger shows instead of just the category's own name, and what
  search matches against, so a same-named category in a different
  branch of the tree is still distinguishable and still findable by
  typing either name.

**Where it's wired in, and why not everywhere:**

- **TransactionDialog's Category field** — hierarchy display _and_
  quick-create (below).
- **TransactionsList's category filter** and **Settings' own "Parent"
  field** (`pages/settings/Categories.tsx`) — hierarchy display only.
  #106's own text scopes quick-create to the transaction dialog
  specifically; the filter has no transaction to lose progress on, and
  Settings already has its own category-creation form immediately above
  the list it's part of.
- **`settings/Accounts.tsx`'s account-type dropdown** and
  **`settings/Categories.tsx`'s own category-type (Expense/Income)
  dropdown** use the plain (non-Combobox) `Select` instead — short,
  flat, two-or-few option lists with no hierarchy and no search need,
  so the lighter primitive is enough; see "Select: Radix, not native"
  above for why they moved off the _native_ `<select>` even so.

**Quick-create**, from the Category combobox only: typing text that
matches no existing category's name (case-insensitively) shows an inline
"Create "…"" row; picking it calls the real `createCategory` API call
(always as a new top-level category of whichever kind the dialog is
currently in — Spend → expense, Receive → income; reparenting it under
something specific is a follow-up trip to Settings, same as any other
category) and slots the result straight into the field as its new
selection, without closing the transaction dialog or discarding anything
already typed. A failed create surfaces via `toast.error` rather than
inline — there's no dedicated error slot inside the combobox's own
create row, and a toast doesn't block or clear the transaction still
mid-entry underneath it — and leaves the popover open with the typed
text still in place, ready to retry.

## Balances: rate-provenance detail row

ADR-0004 flags this as deferred design work: show "converted at 0.0115 on
14 Aug" without cluttering a table. Issue #139 (`pages/Balances.tsx`)
settles it as a **click-to-expand detail row**, not a hover tooltip: the
converted figure ("≈ 1380.00 EUR") is itself a `Collapsible` trigger
(`components/ui/collapsible.tsx`, the same primitive `AppSidebar`'s
Settings submenu already uses) — clicking it expands a row directly
beneath the account, inside the same `<li>`, carrying the full sentence
("1 USD = 0.92 EUR · as of 2026-09-03 · frankfurter"). A stale rate gets
an inline "stale" flag next to the converted figure itself, not only
inside the expanded detail — someone scanning the collapsed table still
sees it.

Tooltip (`components/ui/tooltip.tsx`) was the other option ADR-0004's own
wording suggests, and was deliberately not used here: it's hover-only
(no touch equivalent, and this is a primary mobile-reachable screen),
requires an app-wide `TooltipProvider` that nothing in the app has
needed yet, and its Radix implementation is fussier to drive in tests
(portal + hover timing) than a plain controlled `open` boolean toggled
by a click. A future screen reusing this pattern (the issue's own note:
"whatever scope M5 eventually gets should reuse this same stale/
unconverted/refresh pattern") should default to the same click-to-expand
row unless it has a specific reason a tooltip fits better.

The same screen's **unconverted** accounts (ADR-0004's
mixed-policy-aggregate-forbidden rule: never silently drop a row a
conversion couldn't cover) get a plain inline `text-destructive` "Not
converted — `<reason>`" label next to the original amount, not a
collapsible — there's no provenance to show for a conversion that never
happened, so a detail row would be empty.

**Shared components (issue #145).** The trigger/detail/unconverted pieces
above are extracted into `components/RateProvenance.tsx`
(`RateAmountTrigger`, `RateProvenanceDetail`, `UnconvertedNote`) rather
than living only in `Balances.tsx`, once a second screen
(`pages/TransactionsList.tsx`) needed the identical interaction for a
foreign-currency transaction row's own reporting-currency equivalent.
Deliberately just the trigger button and the detail container, not a
bundled `Collapsible` root: each caller's own `<li>` needs the
`Collapsible` to wrap its _entire_ row (so the expanded detail lands
full-width below it), while the trigger sits nested inside that row next
to the amount — Radix's trigger/content only need to share a `Root` via
context, not be DOM-adjacent, so callers still import `Collapsible`
directly from `components/ui/collapsible` and drop these two pieces
wherever their own layout needs them. Each caller also writes its own
detail sentence rather than the shared component prescribing one —
transfer rows (below) have no `rate_date`/staleness to report at all (a
transfer's implied rate is fixed at write time, never stale), so a shared
sentence shape would have to special-case that anyway. Any future screen
reusing this pattern (M5's stats screens, per issue #145's own note)
should reach for these three exports rather than re-deriving the same
Collapsible/`text-destructive` markup a third time.

**Transfer rows (issue #141).** A cross-currency transfer's own implied
rate — "1 USD = 80.00 INR", ADR-0004's "what this transfer actually
cost" — reuses the same `RateAmountTrigger`/`RateProvenanceDetail` pair,
just with a narrower detail sentence (`rate · rate_source`, no
`rate_date`/stale flag, since the rate is fixed at write time and is
never reconciled against a provider rate for the day). `TransactionsList`
also renders the to-leg's own amount next to the from-leg's
("100.00 USD → 8000.00 INR") once there's a rate to explain the
relationship between them — before this issue, only the from-leg amount
was shown at all. A same-currency transfer gets neither: same single
amount as before, no trigger.

When the reporting currency matches _neither_ leg's own currency, each
leg also gets its own ordinary reporting-currency equivalent — e.g. an
INR→USD transfer viewed with EUR as the reporting currency shows both
"≈ 92.00 EUR" (for the INR leg) and "≈ 83.20 EUR" (for the USD leg). This
is a deliberately separate fact from the implied rate above ("roughly
what this leg is worth in a currency neither leg used" vs. "what the
transfer actually cost") and is never shown when the reporting currency
already matches one of the legs — that leg's own amount already _is_
the reporting-currency figure in that case, and a second, separately-
fetched "equivalent" would just restate the same number under a
different name. To keep the two facts from reading as the same kind of
thing, this hint reuses #138's `useFxConversionHint`/`FxConversionHint`
(TransactionDialog's own read-only, non-collapsible "≈ N as of D" line
with an inline narrow refresh) rather than the implied rate's
Collapsible/trigger chrome — one row per leg, prefixed with that leg's
own account name, always visible rather than tucked behind a click. It
also follows #138's shape for refreshing, not #145's: a narrow
single-currency, single-date (`fetchFxRates([currency], t.date,
reportingCurrency)`) fetch, never a date-range backfill popover, since a
transaction-list row only ever has its own one `booked_date` to refresh
against.

**Refresh popover.** Both the stale flag and the unconverted label share
one "Refresh rates" affordance, now `components/RateFetchPopover.tsx`
(`components/ui/popover.tsx` + `components/ui/checkbox.tsx`, and
`components/ui/date-picker.tsx` when a date range is passed): a button
opens a popover listing every in-use currency as a checkbox, pre-checked
for whichever are actually stale or unconverted, and calling
`POST /api/v1/fx/rates/fetch` (via `fetchFxRates`) only for the ones left
checked on confirm. It's fully controlled — open state, the selected set,
and (when present) the date range all live in the caller — since what
counts as "needs refresh" differs per screen (Balances checks its
currently-converted view; TransactionsList checks its currently-loaded
page) and that logic doesn't belong in the shared chrome.

Balances passes no `dateRange` and always fetches at today's date (the
`current` policy this screen uses has no other sensible date).
TransactionsList (issue #145) is the screen that actually needs the
`from`/`to` range this component supports: a `transaction_date`-policy
list spans many distinct historical dates, so its "Backfill rates"
popover adds a currency multi-select _and_ a date range, defaulting both
to the span of currently-stale/unconverted rows' own booked dates. After
a successful backfill, TransactionsList drops the cached conversion for
every row in one of the backfilled currencies so the per-row lookup runs
again and previously-unconverted/stale rows resolve in place — the same
re-read-after-refresh behaviour Balances' own `loadConverted(...)` call
does after its refresh. Any future date-range screen (M5's stats/
analytics screens, per the issue's own note) should reach for this same
component rather than re-deriving the checkbox-list-plus-range shape.

## Casing enum values for display

The same underlying value can read differently depending on where it
appears, and both are correct for their own context — but each context's
render must come from the _same_ underlying label, not a second
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
`Settings.tsx`), but that function is the _only_ place that value's
display text is written.

## Adding a color

Never invent a color outside this system. A new accent hue: same
chroma and lightness as the existing one, only the hue changes. A new
neutral: same near-zero chroma, same hue (240) as the rest of the gray
scale. If neither fits, that's a sign the need is a new _semantic_
token (like `success` was), not a one-off hex value.

## Charts (issue #189)

`components/ui/chart.tsx`, pulled via `npx shadcn add chart`, is the
only charting primitive in the app — every analytics chart
(`pages/Analytics.tsx`) wraps `recharts` components in `ChartContainer`
rather than reaching for `recharts` directly or a second library. At
install time this pulled `recharts` **3.8.0**: shadcn's own chart docs
still carry a "we're working on upgrading to Recharts v3" note pointing
at an unofficial manual snippet, but the *default*, officially-supported
`add chart` install already resolves to a real v3 release today — no
manual patch was needed, and the unofficial v3 snippet was deliberately
not used. Re-running `npx shadcn add chart` to pick up a future version
should stay on whatever the default install resolves to unless a
concrete v3 incompatibility shows up; don't reach for an unofficial
snippet preemptively.

Five categorical CSS custom properties back every chart's `ChartConfig`
(`index.css`'s `:root`/`.dark`, mirrored into `@theme inline` as
`--color-chart-*`): `--chart-1`/`--chart-2` alias the existing
`--success`/`--destructive` tokens (inflow/outflow already read as
"good"/"bad" everywhere else in the app, e.g. `TransactionsList`), and
`--chart-3`/`--chart-4`/`--chart-5` are new hues spread away from both
for per-category series that need more than two colors. A future chart
needing more series than these five should extend this same palette
(same lightness/chroma family, new hue) rather than picking an
unrelated color, per "Adding a color" above.

`ChartContainer`'s text-node output (recharts renders axis/legend labels
as SVG `<text>`/`<tspan>`, sometimes split across multiple `<tspan>`s
per label) doesn't reliably match `@testing-library`'s exact-text
queries under jsdom's `ResizeObserver` stub (`src/test/setup.ts`) — a
chart's own component test should assert on the screen's plain-HTML
figures (stat cards, headings) rather than on text inside the chart's
SVG itself; see `Analytics.test.tsx`'s own comment on this.
