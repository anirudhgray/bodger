# User guide

This is the guide for actually using `bodger` — not for building it (that's [`contributing.md`](contributing.md)) and not for how it's put together inside (that's [`architecture.md`](architecture.md)).

Right now that means the command line and, if you want it, a REST API served from the same binary. There's no web version yet — you run one binary on your own machine, and it keeps your data in a single file next to it. A web UI, multi-currency support, budgets, and an AI-agent interface come in later milestones — see [Status](architecture.md#9-status) if you want the full roadmap.

---

## Installing and first run

You need Go and Node at the versions pinned in [`.tool-versions`](../.tool-versions) — `asdf install` or `mise install` gets you both.

```sh
make setup   # install dependencies
make build   # produces bin/bodger
```

Run it once with no arguments to check everything's wired up:

```sh
./bin/bodger
bodger: ready — run `bodger --help` to see what you can do
```

The first time you run any command, `bodger` creates a SQLite database file (`bodger.db`, in the directory you ran it from) and sets it up. That one file is everything — your accounts, your categories, every transaction you've recorded. Back it up by copying it; move to a new machine by copying it there.

A few things you can set before you start, if the defaults don't suit you — none of these are required:

| Environment variable | What it controls | Default |
| --- | --- | --- |
| `BODGER_DB_PATH` | Where the database file lives | `bodger.db` in the current directory |
| `BODGER_DEFAULT_CURRENCY` | The currency used when nothing more specific applies | `USD` |
| `BODGER_USER_TIMEZONE` | The timezone "today" and your dates are resolved in | `UTC` |
| `BODGER_HTTP_BIND_ADDR` | Where `bodger serve` (see below) listens | `127.0.0.1:8080` |

For example, if you're in India and want dates and "today" resolved correctly:

```sh
export BODGER_USER_TIMEZONE=Asia/Kolkata
export BODGER_DEFAULT_CURRENCY=INR
```

Set these once (in your shell profile) rather than repeating them per command.

---

## Creating accounts and categories

An **account** is somewhere your money actually sits or is owed — a bank account, a wallet, a credit card. A **category** is what you spend on or earn from — groceries, rent, salary.

```sh
bodger accounts add "HDFC Savings" --type bank --currency INR --opening-balance 15000
bodger accounts add Cash --type cash --currency INR

bodger categories add groceries --type expense
bodger categories add dining --type expense
bodger categories add salary --type income
```

`--type` is required for both. Account types are `bank`, `cash`, `credit_card`, `wallet`, `investment`, `loan`, or `other`. Category types are `expense` or `income`.

`--opening-balance` is what the account held when you started tracking it in `bodger` — leave it out and it starts at zero. You can change it later:

```sh
bodger accounts set-opening-balance "HDFC Savings" 15500 --on 2026-01-01
```

See what you've got:

```sh
bodger accounts list
bodger categories list
```

Categories can sit under one another — `groceries` and `dining` both under `food`, say — so `categories tree` shows the shape of that, where `categories list` shows the flat roll of everything:

```sh
bodger categories tree
```

```
food (expense)
  dining (expense)
  groceries (expense)
salary (income)
```

Got the name wrong, or want to move a category under a different one? Renaming and reparenting change only that — every transaction already recorded against them stays exactly as it was:

```sh
bodger accounts rename "HDFC Savngs" "HDFC Savings"
bodger categories rename groceries food
bodger categories reparent groceries --parent food
bodger categories reparent groceries            # no --parent moves it back to the top level
```

An account or category you no longer use can be archived rather than deleted — it disappears from these lists, but every transaction that already used it stays exactly as it was:

```sh
bodger accounts archive "Old Wallet"
bodger categories archive "Unused Category"
```

---

## Recording an expense, an income, and a transfer

This is the thing you'll do most, so it's built to be quick: an amount and a category is enough — if you've only got one account, that's genuinely all you need:

```sh
bodger spend 800 groceries
```

`bodger` uses your only account, and today's date, automatically. Once you've added a second account (like we did above), say which one with `--account`, and give a date with `--on` if it wasn't today:

```sh
bodger spend 800 groceries --account "HDFC Savings" --on 2026-08-14
```

Money coming in works the same way, with `receive`:

```sh
bodger receive 150000 salary --account "HDFC Savings"
```

Moving money between two of your own accounts is `move` — it needs both ends named, since there's no one obvious account to default to:

```sh
bodger move 20000 --from "HDFC Savings" --to Cash
```

If the two accounts are in different currencies, `bodger` derives and shows the implied exchange rate alongside the transfer. By default that's based on the same number on both ends (reinterpreted in the destination currency), which usually isn't the real rate you got — add `--to-amount` to say exactly how much arrived, and the rate shown reflects that instead:

```sh
bodger move 20000 --from "HDFC Savings" --to "Chase USD" --to-amount 230
```

If you have more than one account and don't say which with `--account` (for `spend`/`receive`) or `--from`/`--to` (for `move`), `bodger` tells you it can't guess and lists your accounts so you can try again.

A couple of extra flags work on all three:

```sh
bodger spend 450 dining --account Cash --tag "date-night" --tag weekend --note "Anniversary dinner"
```

`--tag` can be repeated. `--note` is free text for anything you want to remember about the entry.

**Recording a split** (one expense across several categories, like a shopping trip that's part groceries and part household goods) isn't built yet — it's planned for the next milestone.

---

## Seeing what you've recorded

```sh
bodger transactions list
```

```
DATE        TYPE   AMOUNT      DESCRIPTION                 ID
2026-08-14  move   500.00 INR  Transfer from Bank to Cash  b2479c06-4f58-4299-adc5-596d70ea792c
2026-08-14  spend  800.00 INR  groceries                   cc033a79-96e6-431c-87d8-cfbe3570bab6
```

Newest first. Narrow it down with any combination of an account, a category (which includes anything filed under it), a type, and a date range:

```sh
bodger transactions list --account "HDFC Savings" --category food --since 2026-08-01 --until 2026-08-31
bodger transactions list --type spend
```

`--type` takes the same words you record with: `spend`, `receive`, or `move`.

Long lists come back a page at a time. `--limit` sets how many you see at once and `--offset` how many to skip — when there might be more, the output tells you the `--offset` to pass for the next page:

```sh
bodger transactions list --limit 20 --offset 20
```

---

## Fixing and deleting transactions

Recorded something wrong? Fix it in place — there's no reversing entry to make, and the correction is what counts from then on:

```sh
bodger transactions edit cc033a79-96e6-431c-87d8-cfbe3570bab6 \
  --amount 950 --description "Weekly shop" --account Cash --category groceries --on 2026-08-13
```

**An edit replaces the whole transaction**, so pass every value you want it to end up with — anything you leave out is cleared, not kept. `bodger transactions list --json` shows you what's there now. `--amount` and `--description` are always required; use `--account`, `--category`, and `--currency` for money you spent or received, and `--from` and `--to` for a move. What you can't change is which of the three it is: money you spent stays money you spent — delete it and record it again if that's what you need.

For a move, `--amount` is what left the `--from` account and `--to-amount` is what arrived in the `--to` account — the same split `transactions list --json`'s `amount`/`to_amount` fields show, so re-editing a cross-currency move with the values it just listed keeps both ends exactly as they were.

Recorded something that never happened? Delete it:

```sh
bodger transactions delete cc033a79-96e6-431c-87d8-cfbe3570bab6
```

It stops counting towards your balances and drops out of your lists immediately. Nothing is erased from your database, so you keep a record of what was there — there's no confirmation prompt for the same reason.

---

## Reading balances

```sh
bodger balance
```

Shows what every account holds right now. Add `--on` to see what it held on some other date:

```sh
bodger balance --on 2026-01-01
```

For your overall financial position instead of one row per account, use `bodger balance totals`: your overall net balance (liabilities like a credit card or loan reduce it, the way they should), a breakdown by account type (bank, cash, credit card, and so on), and a breakdown by currency (each currency's own accounts summed in that currency, with no conversion). `--policy` is required here — the totals are always converted into one currency, unlike `bodger balance`'s own per-account figures, which stay meaningful unconverted:

```sh
bodger balance totals --policy current
```

---

## Getting output a script can read

Every command that returns data — `balance`, `accounts list`, `spend`, and so on — accepts `--json` for machine-readable output instead of the plain-text version:

```sh
bodger balance --json
```

```json
{
  "data": {
    "as_of": "2026-08-14",
    "balances": [
      { "account_id": "...", "account": "HDFC Savings", "currency": "INR", "balance": "14700.00", "archived": false }
    ]
  }
}
```

An error comes back the same shape either way — as plain text normally, or as `{"error": {...}}` under `--json` — with a stable `code` your script can branch on regardless of the wording of the message.

---

## Running the REST API

`bodger serve` starts a REST API server backed by the same database and the same application logic as the command line — nothing about how a transaction is recorded or validated differs between the two.

```sh
bodger serve
bodger: listening on 127.0.0.1:8080
```

It binds `127.0.0.1` (loopback) by default. Set `BODGER_HTTP_BIND_ADDR` if you need a different loopback address or port; a non-loopback address is refused until you've set a password (see below) — if you want it reachable from another machine before that, put it behind something that handles authentication itself (a reverse proxy, a VPN) rather than exposing it directly.

Every resource the CLI can touch has an equivalent under `/api/v1`: accounts, categories, transactions, transfers, balances, exchange rates, and your reporting currency, plus `/healthz` to check the server is up and `/api/v1/auth/*` for the routes below. A request with no `date` field books to today the same way `spend`/`receive`/`move` do — resolved on the server, in your configured timezone, never by the client. Amounts are always sent and returned as plain decimal strings with a separate currency field, never as numbers.

```sh
curl http://127.0.0.1:8080/api/v1/accounts
curl -X POST http://127.0.0.1:8080/api/v1/transactions \
  -d '{"type":"outflow","account":"HDFC Savings","category":"groceries","amount":"800","description":"Groceries"}'
```

The full set of routes, request and response shapes, and error codes is described in the project's OpenAPI document (`internal/surface/http/openapi.json`) — point any OpenAPI-aware tool at that file for a browsable reference.

### Signing in and API tokens

Every route above requires a credential — first set a password with `bodger auth set-password` (run on the machine hosting `bodger.db`; see `bodger auth set-password --help`), then sign in:

```sh
curl -c cookies.txt -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -d '{"password":"your-password"}'
```

A successful login sets a session cookie (that's what `-c cookies.txt` saves) — the browser-based web UI uses exactly this. Send it back on later requests with `-b cookies.txt`. Any request that changes something (anything but a `GET`) also needs a `X-Bodger-CSRF` header alongside the cookie — its value doesn't matter, only its presence:

```sh
curl -b cookies.txt -X POST http://127.0.0.1:8080/api/v1/transactions \
  -H 'X-Bodger-CSRF: 1' \
  -d '{"type":"outflow","account":"HDFC Savings","category":"groceries","amount":"800","description":"Groceries"}'
```

`POST /api/v1/auth/logout` ends the current session; `POST /api/v1/auth/logout-all` ends every session you have open, on every device, in one call. `POST /api/v1/auth/password` changes your password from within a signed-in session (`{"new_password":"..."}`) — this also signs you out everywhere, including the request that made the change, so you'll need to sign in again with the new password afterward.

For a script, a cron job, or any client that isn't a browser, an **API token** is usually easier than a cookie — it's a long-lived credential you send as `Authorization: Bearer <token>` and never needs the CSRF header:

```sh
curl -b cookies.txt -X POST http://127.0.0.1:8080/api/v1/auth/tokens \
  -H 'X-Bodger-CSRF: 1' -d '{"name":"backup script"}'
# {"data":{"id":"...","name":"backup script","created_at":"...","token":"bdg_..."}}

curl http://127.0.0.1:8080/api/v1/accounts \
  -H 'Authorization: Bearer bdg_...'
```

The `token` field is shown exactly once, in that create response — store it somewhere safe, since it can't be retrieved again. `GET /api/v1/auth/tokens` lists every token you've created (including expired and revoked ones, for history), and `DELETE /api/v1/auth/tokens/{id}` revokes one immediately. You can also manage API tokens from the command line with `bodger auth token create`/`list`/`revoke`.

### Multi-currency and exchange rates

Add `currency` and `policy` to `GET /api/v1/balances` to convert every account's balance into one currency instead of seeing each in its own. `policy` picks which date's exchange rate is used: `transaction_date` uses each balance's own as-of date, `current` always uses today's rate, and `pinned` uses one date you choose with `pinned_date`. Each converted balance comes back with the rate, its source, and the date it was recorded for, so you can always see what it's based on; an account whose currency has no stored rate close enough to use is listed separately under `unconverted` rather than silently left out of the total.

```sh
curl -H 'Authorization: Bearer bdg_...' \
  'http://127.0.0.1:8080/api/v1/balances?currency=USD&policy=current'
```

You can also do this from the command line with `bodger balance --currency --policy`.

`GET /api/v1/balances/totals` returns your overall net balance and a breakdown by account type — both converted into `currency` under `policy` (`policy` is required here; `currency` defaults to your reporting currency when omitted) — plus a breakdown by currency, which is always raw and unconverted. An account the conversion couldn't cover is reported under `unconverted` the same way `GET /api/v1/balances` does.

```sh
curl -H 'Authorization: Bearer bdg_...' \
  'http://127.0.0.1:8080/api/v1/balances/totals?policy=current'
```

You can also do this from the command line with `bodger balance totals --policy`.

`GET /api/v1/fx/rates` looks up the exchange rate between two currencies straight from what's already stored, without ever reaching out to the network. Add `amount` to also get a converted figure back alongside the rate:

```sh
curl -H 'Authorization: Bearer bdg_...' \
  'http://127.0.0.1:8080/api/v1/fx/rates?from=INR&to=USD&policy=current&amount=1000'
```

You can also do this from the command line with `bodger fx rates list`.

`POST /api/v1/fx/rates/fetch` is the one route that actually reaches out to your configured rate provider and stores what it gets back. With no `pairs`, it fetches every currency pair you actually use, quoted against your reporting currency, at today's date; pass `pairs` to restrict it to specific currencies, `quote` to quote them against a currency other than your reporting currency, and `from`/`to` together for a historical backfill instead of just today:

```sh
curl -X POST -H 'Authorization: Bearer bdg_...' \
  http://127.0.0.1:8080/api/v1/fx/rates/fetch \
  -d '{"pairs":["INR"],"quote":"EUR"}'
```

You can also do this from the command line with `bodger fx rates fetch`.

`GET /api/v1/reporting-currency` and `POST /api/v1/reporting-currency` read and set the currency your balances and reports convert into by default:

```sh
curl -X POST -H 'Authorization: Bearer bdg_...' \
  http://127.0.0.1:8080/api/v1/reporting-currency \
  -d '{"currency":"USD"}'
```

You can also do this from the command line with `bodger config reporting-currency get`/`set`.

### Using the web UI

With `bodger serve` running, open its address in a browser (`http://127.0.0.1:8080` by default). Visiting it without a signed-in session lands you on a login screen; enter the password you set with `bodger auth set-password` — there's no separate web password, it's the same credential the CLI and REST API use. A successful login takes you into the app itself; visiting the login page again while already signed in just sends you straight back in. Transactions, Balances, Analytics, and Settings live in a sidebar — always visible on a wider screen, tucked behind the icon in the top-left corner on a narrow one. The sun/moon button switches between light and dark mode; your choice is remembered on that browser for next time, overriding your OS/browser's own light/dark setting. The "Log out" button next to it ends your session and returns you to the login screen. The **Balances** tab shows what every account holds as of today, the same figures `bodger balance` prints on the command line, plus a totals overview — your overall net balance, a breakdown by account type, and (once more than one currency is in play) a breakdown by currency — the same figures `bodger balance totals` prints.

**Recording a transaction** works the same way it does at the command line: pick Spend, Receive, or Move, and fill in the amount, category (or the two accounts, for a move), and account — three fields is all it takes, and if you've only got one account it's preselected for you. The category field shows how your categories nest under one another (a "Groceries" tucked under "Food" reads as such, not as an unrelated name in a flat list) and you can type to search instead of scrolling. If the one you want doesn't exist yet, typing its name offers to create it right there — pick that option and it's created and selected in one step, without losing anything else you've already filled in. The date defaults to today unless you open "Add details," which also has fields for notes and tags. Nothing you've typed is lost if the server rejects the entry (a category that doesn't exist, say) — fix the problem and submit again.

If you use more than one currency, an amount you enter in a currency other than your reporting currency shows a small "≈" line underneath with the converted figure and the date it's based on — a preview only, never saved with the transaction. It's keyed to whichever date the transaction is booked to (today by default, or whatever you pick under "Add details"), not to today, so backdating an entry shows the rate as of that day. If nothing's been fetched for that day yet, or the stored rate needs updating, a "Fetch rate"/"Refresh" link next to it pulls just that one currency for that one day — never every currency you use. None of this appears if every account you have uses the same currency.

A **Move** between two accounts in different currencies shows a second "Amount received" field for the destination account's own currency, suggested from a stored rate when one's available but always yours to edit — the figure that actually gets saved is whatever the two amounts end up being, not the suggested rate. Leave it blank and the destination amount is worked out from the amount you typed, same as before this existed.

The **Transactions** screen lists what you've recorded, newest first. Click **Filters** to narrow it down by account, category, type, or date range — the same filters `bodger transactions list` offers, tucked one click away rather than shown by default; the category filter shows the same nesting and search as the transaction form's own category field. Click **Edit** on any row to correct it in place — as with the CLI, an edit replaces the whole transaction, so the form starts pre-filled with everything it currently has; change what's wrong and save. **Delete** removes a transaction immediately, with no confirmation prompt — same as the CLI, nothing is erased from your database, so you keep a record of what was there.

If you use more than one currency, a spend or receive recorded in a currency other than your reporting currency shows its own small "≈" figure next to the amount — click it to expand the exchange rate it's based on and the date that rate was recorded for, the same click-to-expand row Balances uses. It's keyed to that transaction's own booked date, not today, so a transaction from months ago is checked against a rate for that day. A row with no usable rate says so ("Not converted") instead of showing nothing. **Backfill rates**, next to Filters, opens a currency and date-range picker and fetches rates for whichever you pick — unlike Balances' own "Refresh rates" (always today), this one is for filling in rates across a stretch of past dates; it starts pre-selecting whichever currencies and dates on the page actually need it. Once it finishes, any row that was showing "Not converted" or a stale rate updates in place. This doesn't apply to a **Move** between accounts — a transfer's own exchange rate is fixed when it's recorded (see "Recording a transaction" above), not looked up per row.

The **Analytics** screen charts your spending and income by category, your cash flow month by month, how this month compares to last, and your savings rate — the same figures `bodger report` prints on the command line, computed the same way. Category spending and income each get their own donut chart, with a sortable table of the same rows underneath (click a column heading to sort by it, click again to reverse). Pick a date range and a currency at the top; leaving the dates blank charts everything you've ever recorded. If a chart can't convert something into your chosen currency, it's listed under "Not converted" rather than silently left out of the total, with a **Refresh rates** button to fetch whatever's missing.

The **Settings** area covers everything about your account and ledger setup, split into its own page per section with a tab strip across the top to switch between them. It's also expandable straight from the sidebar — click **Settings** there to reveal Password, API tokens, Accounts, Categories, and Currency without visiting the page first:

- **Password** — change it without going back to the CLI. This signs you out everywhere, including the browser you just used, so you'll land back on the login screen afterward.
- **API tokens** — create, list, and revoke them, the same as `bodger auth token create`/`list`/`revoke`. A newly created token's value is shown once, right there on the screen — copy it before navigating away, since it can't be shown again.
- **Accounts** and **Categories** — each on its own page: add, rename, and archive either, and move a category under a different parent (or back to the top level). The category list is shown as a tree, each one indented under its parent, matching `bodger categories tree` on the command line; the "Parent" field you reparent from shows that same nesting. This is the same CRUD the CLI's `bodger accounts` and `bodger categories` commands expose, for whenever a browser is more convenient than a terminal.
- **Currency** — set the currency balances and reports convert into, the same as `bodger config reporting-currency get`/`set`. If you've never set one, the field shows as empty and the page tells you it's falling back to this instance's default.

---

## Setting a password and managing API tokens

Set your password from the command line — there's no web sign-up form, and no email involved:

```sh
bodger auth set-password
New password:
Confirm password:
Password updated.
```

Typed input is hidden. If you ever forget your password, run this same command again on the machine hosting your database — no existing password is required, so this doubles as the recovery path.

Scripts, cron jobs, or a `bodger` running against a remote server authenticate with an API token instead of a password:

```sh
bodger auth token create "backup script"
Created API token "backup script".
id: 3f9e2b7a-...

bdg_C-Uh_kszWlzzkuaYEHnGGmYU56xng9no04bVulG12A0

This is the only time the token is shown. Store it now — bodger keeps only its hash.
```

The plaintext token is shown exactly once, at creation. `bodger auth token list` shows every token you've created — name, creation date, last use, expiry, and whether it's been revoked — but never the plaintext again. `bodger auth token revoke <id>` disables one immediately.

```sh
bodger auth token list
bodger auth token revoke 3f9e2b7a-...
```

Add `--expires <date>` to `token create` to give a token a fixed expiry instead of one that lasts until you revoke it.

---

## Command reference

All commands default to plain-text output; add `--json` to any of them for machine-readable output instead.

| Command | What it does |
| --- | --- |
| `bodger spend <amount> <category> [--account] [--on] [--tag] [--note]` | Record money you spent |
| `bodger receive <amount> <category> [--account] [--on] [--tag] [--note]` | Record money you received |
| `bodger move <amount> --from <account> --to <account> [--to-amount] [--on] [--tag] [--note]` | Move money between two of your accounts — accounts in different currencies are fine, and the implied exchange rate is shown alongside the transfer; add `--to-amount` to state exactly how much arrived instead of reusing `<amount>`'s digits |
| `bodger balance [--on] [--currency] [--policy] [--pinned-date]` | See what every account holds — add `--currency`/`--policy` to convert every balance into one currency, with the rate shown alongside |
| `bodger balance totals --policy [--on] [--currency] [--pinned-date]` | Your overall net balance, plus breakdowns by account type and by currency |
| `bodger transactions list [--account] [--category] [--type] [--since] [--until] [--limit] [--offset]` | List what you've recorded, newest first |
| `bodger transactions edit <id> --amount <amount> --description <text> [--account] [--category] [--currency] [--from] [--to] [--to-amount] [--on] [--tag] [--note]` | Correct a transaction — replaces every value |
| `bodger transactions delete <id>` | Delete a transaction you recorded by mistake |
| `bodger accounts list` | List your accounts |
| `bodger accounts add <name> --type <type> [--currency] [--opening-balance] [--opening-balance-date] [--institution] [--sort-order]` | Add an account |
| `bodger accounts rename <account> <new-name>` | Rename an account |
| `bodger accounts archive <account>` | Archive an account |
| `bodger accounts set-opening-balance <account> <amount> [--on]` | Re-declare an account's starting balance |
| `bodger categories list` | List your categories |
| `bodger categories tree` | Show your categories with what sits under what |
| `bodger categories add <name> --type <type> [--parent] [--sort-order]` | Add a category |
| `bodger categories rename <category> <new-name>` | Rename a category |
| `bodger categories reparent <category> [--parent]` | Move a category under a different one, or to the top level |
| `bodger categories archive <category>` | Archive a category |
| `bodger serve` | Start the REST API server |
| `bodger auth set-password` | Set or change your password |
| `bodger auth token create <name> [--expires]` | Create a new API token |
| `bodger auth token list` | List your API tokens |
| `bodger auth token revoke <id>` | Revoke an API token |
| `bodger fx rates fetch [--pair] [--quote] [--from] [--to]` | Fetch and store exchange rates from the configured provider — defaults to every currency pair you actually use, quoted against your reporting currency; add `--quote` to quote `--pair` entries against a different currency, and `--from`/`--to` for a historical backfill |
| `bodger fx rates list --from <currency> --to <currency> --policy <policy> [--transaction-date] [--pinned-date] [--amount]` | Look up the exchange rate between two currencies under a given conversion policy, optionally converting an amount |
| `bodger config reporting-currency get` | See your configured reporting currency |
| `bodger config reporting-currency set <currency>` | Set your reporting currency |

`<account>` and `<category>` accept either the name you gave it (case-insensitive) or its ID. If a name matches more than one of your accounts or categories, `bodger` lists the candidates instead of guessing. `<id>` is a transaction's own ID, which `bodger transactions list` shows in its last column.

---

## In the meantime

- **What is this?** → [`README.md`](../README.md)
- **How does it work?** → [`architecture.md`](architecture.md)
- **What does it store, and what do the words mean?** → [`data-model.md`](data-model.md)
- **How do I build it?** → [`contributing.md`](contributing.md)

The web UI, multi-currency, reports, budgets, and the MCP server all land here in the same PR that ships them, per [`contributing.md`](contributing.md) — not in a catch-up pass afterwards.
