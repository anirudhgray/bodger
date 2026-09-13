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

For how your net worth has moved over time rather than a single figure, use `bodger balance net-worth`: one point per week, month, or year (your choice, via `--granularity`) across a date range you pick, plus everything `bodger balance totals` already reports being unconvertible. Both `--from` and `--to` are required — there's no default range for a time series:

```sh
bodger balance net-worth --from 2026-01-01 --to 2026-09-01 --policy current --granularity month
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

## Exporting your data

`bodger export json` downloads everything you have — every account, category, and transaction — as one JSON file. This is the format to keep as a backup: it's the only one a re-import can restore from exactly.

```sh
bodger export json -o backup.json
```

Leave off `-o` to print the file to standard output instead of writing it — useful for piping straight into another command or a script.

`bodger export csv` downloads your transactions as a spreadsheet-friendly CSV, one row per transaction (a transaction split across more than one category becomes several rows sharing the same date and description). It accepts the same filter flags as `bodger report` — see the Command reference below — so you can export just one account, one category, or one date range:

```sh
bodger export csv --account "HDFC Savings" --from 2026-08-01 --to 2026-08-31 -o august.csv
```

CSV is for spreadsheets, not backups — a split transaction can't be told apart from two separate ones once it's flattened into rows. Use `bodger export json` if you ever need to restore your data.

The REST API offers the same two downloads at `GET /api/v1/export/json` and `GET /api/v1/export/csv`.

In the web UI, **Import & export** covers all three (Import, Export, Restore) split into their own pages with a tab strip across the top to switch between them, the same shape as Settings — it's also expandable straight from the sidebar. **Export** has a "Download backup (JSON)" button for the same full backup, and a second card for the CSV download with the same filters as the Transactions screen (account, category, type, and date range) — leave them all on "Any" for every transaction.

### Restoring from a backup

**`bodger restore` overwrites everything you currently have.** It replaces every account, category, and transaction with whatever is in the backup file — this is not a merge, and it cannot be undone. Only run it against a file you trust, and only when you actually mean to discard your current data.

```sh
bodger restore backup.json --yes
```

`--yes` is required — without it, `bodger restore` refuses to run and doesn't touch anything. Leave off the file argument to read the backup from standard input instead. Only a JSON backup produced by `bodger export json` can be restored; a CSV export can't be, since it doesn't carry enough information to rebuild your data exactly.

The REST API offers the same operation at `POST /api/v1/restore`. Its request body carries the backup document under `"document"` and must set `"confirm": true`; a request without it is rejected before anything is touched:

```sh
curl -X POST http://127.0.0.1:8080/api/v1/restore \
  -d "{\"confirm\": true, \"document\": $(cat backup.json)}"
```

The web UI's **Restore** page carries the same weight the CLI's `--yes` flag does, deliberately made harder to trigger by accident than a single click: a warning banner explains what's about to happen, choosing a backup file (click to browse or drag and drop) only reads and checks it's valid JSON, and the "Replace everything…" button that follows opens a dialog asking you to type an exact confirmation phrase — shown right there — before its own "Replace everything" button enables. Nothing is sent until that phrase matches exactly. A failure (a backup missing its format version, say) is shown inside that same dialog without losing your place; success shows how many accounts, categories, and transactions were installed and a link straight to Transactions.

---

## Importing a bank statement

`bodger import upload` reads a CSV file from your bank or card and stages it for review — nothing is added to your accounts yet. You tell it which account the file belongs to, and which column holds which field:

```sh
bodger import upload statement.csv \
  --account "HDFC Savings" \
  --date-column Date --description-column Description --amount-column Amount
```

This prints a summary: how many rows are ready to go in as-is, how many were skipped as exact duplicates of something you already have, and how many need your attention because they look like they might be duplicates of an existing transaction. It also prints the import's own ID, which every other `bodger import` command below takes.

See what was staged, including any rows flagged for review:

```sh
bodger import records <import-id>
```

A row flagged as a possible duplicate needs a decision before you can commit:

```sh
bodger import resolve <record-id> --resolution not_duplicate       # keep it
bodger import resolve <record-id> --resolution confirmed_duplicate # leave it out
```

Once every flagged row has been resolved, commit the import to actually record the transactions:

```sh
bodger import commit <import-id>
```

If something went wrong — the wrong account, a bad column mapping — undo it:

```sh
bodger import rollback <import-id>
```

This removes exactly the transactions that import created; everything else you've recorded is untouched. `bodger import list` shows every import you've started, and `bodger import show <import-id>` looks up one import's current status.

The REST API offers the same operations: `POST /api/v1/imports` (the uploaded file's own bytes as the request body, with the account, filename, and column mapping as query parameters), `GET /api/v1/imports`, `GET /api/v1/imports/{id}`, `GET /api/v1/imports/{id}/records`, `POST /api/v1/import-records/{id}/resolve`, `POST /api/v1/imports/{id}/commit`, and `POST /api/v1/imports/{id}/rollback`.

Only CSV is supported today.

The web UI's **Import** page (under the sidebar's **Import & export** entry) walks through the same staged pipeline as a short wizard. "New import" starts it: choose the account first, then a file (click to browse or drag and drop) — nothing is uploaded until you continue. The next step matches each field (Date, Description, and Amount are required; Posted date, Currency, Transaction ID, and Category are optional) to a column from your file's own header row, pre-guessed from common column names but always yours to change, then "Stage for review" uploads it. The review table that follows lists every staged row with its amount and any flags — a suspected duplicate needs "Not a duplicate" or "Confirm duplicate" before you can commit; an exact duplicate is shown as already skipped, and a possible transfer is informational only. "Commit import" is disabled until every flagged row has a decision. **Import history**, below, lists every import you've started with its status; a staged or reviewed one has a "Continue review" button to pick back up where you left off, and a committed one has "Roll back" to undo it — a confirmation dialog first, since rolling back deletes the transactions it created (they keep their history, the same as deleting any other transaction).

---

## Managing budgets

A budget plans an amount for a category over a monthly cycle, kept separate from what you actually spend so you can compare the two. Create one with a name, a currency, and the date its periods are computed from:

```sh
bodger budgets add "Groceries Budget" --currency USD --starts-on 2026-08-01
```

This creates an empty budget — add lines to it one at a time, each planning an amount for one category:

```sh
bodger budgets lines add <budget-id> groceries 500
```

A line's category can't change once created; remove it and add a new one instead if you need to plan a different category. `--rollover` marks a line's unspent (or overspent) amount to carry into the next period — recorded today, but not yet acted on by anything (a future release will use it).

`bodger budgets list` lists every budget you have, archived ones included; `bodger budgets show <budget-id>` shows one budget and its lines. `bodger budgets update <budget-id> <name> [--starts-on]` renames a budget and sets its starts-on date together in one call — if you leave off `--starts-on`, it resolves to today, not to the budget's existing date, so pass the current one back explicitly if you only mean to rename. `bodger budgets archive <budget-id>` hides a budget from listings while keeping its history — including past actuals — fully queryable. A budget's currency and period type (monthly, the only kind today) are fixed once created.

See how actual spending compares to what you planned for a period — defaulting to the current month:

```sh
bodger budgets actuals <budget-id>
bodger budgets actuals <budget-id> --period 2026-07-15   # any date in July
```

For each line, this reports the budgeted amount, actual net spend against that category (and everything nested under it) for the period, what's remaining, and utilisation (actual ÷ budgeted), plus an overall figure that sums every line into one budgeted/actual/remaining/utilisation for the whole budget. Actual spend already converts a different-currency transaction into the budget's own currency using the rate as of its own transaction date; anything that couldn't be converted is listed under `unconverted` rather than silently dropped. The report also states the date it's "as of" (today, in your configured timezone) — comparing it against the period's own start and end tells you whether you're looking at the current month, a past one, or a future one. `bodger budgets history <budget-id> [--period] [--months]` repeats this over several consecutive months (6 by default) ending at `--period`'s month, oldest first — a budget's own starts-on date clamps how far back this goes, so a budget that hasn't existed that long reports fewer months rather than erroring.

The REST API offers the same operations: `GET /api/v1/budgets`, `POST /api/v1/budgets` (optionally with an initial batch of lines in the request body, alongside adding them one at a time afterward), `GET /api/v1/budgets/{id}`, `PATCH /api/v1/budgets/{id}`, `DELETE /api/v1/budgets/{id}` (archive), `POST /api/v1/budgets/{id}/lines`, `PATCH /api/v1/budgets/{id}/lines/{lineId}`, `DELETE /api/v1/budgets/{id}/lines/{lineId}`, `GET /api/v1/budgets/{id}/actuals`, and `GET /api/v1/budgets/{id}/history`.

```sh
curl -X POST -H 'Authorization: Bearer bdg_...' \
  http://127.0.0.1:8080/api/v1/budgets \
  -d '{"name":"Groceries Budget","currency":"USD","starts_on":"2026-08-01"}'
```

The web UI's **Budgets** screen (in the sidebar) shows every active budget as its own card: an **Overall** bar summing every line into one budgeted/actual figure for the whole budget, followed by a table of the lines themselves with category, budgeted, actual, remaining, and their own utilisation bar alongside the percentage — colored and labeled "Under budget"/"At budget"/"Over budget" so a line approaching or past its limit stands out at a glance. When you're looking at the month currently in progress, every bar (Overall and each line) also shows a marker for how far through the month today is; hover it for the exact day count. A line's fill sitting past that marker is a sign you're ahead of pace even if the percentage alone still reads "under budget" (the marker doesn't appear for a past or future month, since "how far through" only means something for the one happening now). Previous/Next above the cards moves the whole screen a month at a time, the same period-history data `bodger budgets history` prints. **New budget** (or, with no budgets yet, the empty state's own button) opens a dialog for the name, currency, and starts-on date, plus an "Add line" row per category you want to plan for — a category picker with the same nesting and search as the transaction form's own. **Edit** on a budget's card reopens that same dialog pre-filled, where currency shows as fixed text instead of an input (it can't change once created) and lines can be added, have their amount or rollover flag changed, or be removed; changing a line's category is done by removing it and adding a new one, same as the CLI. **Archive** hides a budget from the overview while keeping its history queryable, with no separate confirmation step, same as an account or category archive elsewhere in the app.

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

With `bodger serve` running, open its address in a browser (`http://127.0.0.1:8080` by default). Visiting it without a signed-in session lands you on a login screen; enter the password you set with `bodger auth set-password` — there's no separate web password, it's the same credential the CLI and REST API use. A successful login takes you into the app itself; visiting the login page again while already signed in just sends you straight back in. Transactions, Balances, Analytics, Budgets, Import & export, and Settings live in a sidebar — always visible on a wider screen, tucked behind the icon in the top-left corner on a narrow one. Import & export and Settings each expand in place to their own sub-items — a second, quicker way into a page you're not on yet, alongside that page's own tab strip once you're there (see below). The sun/moon button switches between light and dark mode; your choice is remembered on that browser for next time, overriding your OS/browser's own light/dark setting. The "Log out" button next to it ends your session and returns you to the login screen. The **Balances** tab shows what every account holds as of today, the same figures `bodger balance` prints on the command line, plus a totals overview — your overall net balance, a breakdown by account type, and (once more than one currency is in play) a breakdown by currency — the same figures `bodger balance totals` prints. Below that, **Net worth over time** charts your total net worth across a date range you pick, bucketed weekly, monthly, yearly, or by a custom range — the same figures `bodger balance net-worth` prints; pick both a From and a To date to see it (there's no default range for a time series).

**Recording a transaction** works the same way it does at the command line: pick Spend, Receive, or Move, and fill in the amount, category (or the two accounts, for a move), and account — three fields is all it takes, and if you've only got one account it's preselected for you. The category field shows how your categories nest under one another (a "Groceries" tucked under "Food" reads as such, not as an unrelated name in a flat list) and you can type to search instead of scrolling. If the one you want doesn't exist yet, typing its name offers to create it right there — pick that option and it's created and selected in one step, without losing anything else you've already filled in. The date defaults to today unless you open "Add details," which also has fields for notes and tags. Nothing you've typed is lost if the server rejects the entry (a category that doesn't exist, say) — fix the problem and submit again.

If you use more than one currency, an amount you enter in a currency other than your reporting currency shows a small "≈" line underneath with the converted figure and the date it's based on — a preview only, never saved with the transaction. It's keyed to whichever date the transaction is booked to (today by default, or whatever you pick under "Add details"), not to today, so backdating an entry shows the rate as of that day. If nothing's been fetched for that day yet, or the stored rate needs updating, a "Fetch rate"/"Refresh" link next to it pulls just that one currency for that one day — never every currency you use. None of this appears if every account you have uses the same currency.

A **Move** between two accounts in different currencies shows a second "Amount received" field for the destination account's own currency, suggested from a stored rate when one's available but always yours to edit — the figure that actually gets saved is whatever the two amounts end up being, not the suggested rate. Leave it blank and the destination amount is worked out from the amount you typed, same as before this existed.

The **Transactions** screen lists what you've recorded, newest first. Click **Filters** to narrow it down by account, category, type, or date range — the same filters `bodger transactions list` offers, tucked one click away rather than shown by default; the category filter shows the same nesting and search as the transaction form's own category field. Click **Edit** on any row to correct it in place — as with the CLI, an edit replaces the whole transaction, so the form starts pre-filled with everything it currently has; change what's wrong and save. **Delete** removes a transaction immediately, with no confirmation prompt — same as the CLI, nothing is erased from your database, so you keep a record of what was there.

If you use more than one currency, a spend or receive recorded in a currency other than your reporting currency shows its own small "≈" figure next to the amount — click it to expand the exchange rate it's based on and the date that rate was recorded for, the same click-to-expand row Balances uses. It's keyed to that transaction's own booked date, not today, so a transaction from months ago is checked against a rate for that day. A row with no usable rate says so ("Not converted") instead of showing nothing. **Backfill rates**, next to Filters, opens a currency and date-range picker and fetches rates for whichever you pick — unlike Balances' own "Refresh rates" (always today), this one is for filling in rates across a stretch of past dates; it starts pre-selecting whichever currencies and dates on the page actually need it. Once it finishes, any row that was showing "Not converted" or a stale rate updates in place. This doesn't apply to a **Move** between accounts — a transfer's own exchange rate is fixed when it's recorded (see "Recording a transaction" above), not looked up per row.

The **Analytics** screen charts your spending and income by category, your cash flow, how the current period compares to the previous one, and your savings rate — the same figures `bodger report` prints on the command line, computed the same way. Category spending and income each get their own donut chart, with a sortable table of the same rows underneath (click a column heading to sort by it, click again to reverse). Pick a date range, a currency, and a **Granularity** (week, month, year, or custom) at the top; leaving the dates blank charts everything you've ever recorded. Granularity controls both how cash flow buckets its bars (a week, a month, or a year at a time) and what "current vs. previous" means for the trends comparison — week compares this week against last week, year compares this year against last year, and custom compares your chosen date range against the immediately preceding range of the same length (both From and To are required for custom; the picker prompts for them if either is missing). Month is the default and matches the screen's original month-over-month behavior. Below the trends and savings-rate cards, **Top transactions** lists your largest transactions in the period at a glance, **Average transaction size** shows the mean transaction amount overall and per category, and **Category trends** breaks the trends comparison down per category instead of just in aggregate — the same figures `bodger report top-transactions`, `bodger report average-transaction-size`, and `bodger report category-trends` print, respecting the same date range, currency, and granularity as the rest of the screen. If a chart can't convert something into your chosen currency, it's listed under "Not converted" rather than silently left out of the total, with a **Refresh rates** button to fetch whatever's missing.

`bodger report cash-flow`, `bodger report trends`, and `bodger report category-trends` take a matching `--granularity week|month|year|custom` flag (default `month`); `bodger report category-breakdown`, `bodger report savings-rate`, and `bodger report average-transaction-size` are unaffected since they already report over an arbitrary date range with no period to bucket or compare. `bodger report top-transactions` also takes `--limit` (default 10, capped at 100) to control how many transactions come back.

The **Settings** area covers everything about your account and ledger setup, split into its own page per section with a tab strip across the top to switch between them. It's also expandable straight from the sidebar — click **Settings** there to reveal Password, API tokens, Accounts, Categories, and Currency without visiting the page first:

- **Password** — change it without going back to the CLI. This signs you out everywhere, including the browser you just used, so you'll land back on the login screen afterward.
- **API tokens** — create, list, and revoke them, the same as `bodger auth token create`/`list`/`revoke`. A newly created token's value is shown once, right there on the screen — copy it before navigating away, since it can't be shown again.
- **Accounts** and **Categories** — each on its own page: add, rename, and archive either, and move a category under a different parent (or back to the top level). The category list is shown as a tree, each one indented under its parent, matching `bodger categories tree` on the command line; the "Parent" field you reparent from shows that same nesting. This is the same CRUD the CLI's `bodger accounts` and `bodger categories` commands expose, for whenever a browser is more convenient than a terminal.
- **Currency** — set the currency balances and reports convert into, the same as `bodger config reporting-currency get`/`set`. If you've never set one, the field shows as empty and the page tells you it's falling back to this instance's default.

---

## Connecting an MCP client to bodger

`bodger mcp` lets an AI assistant or agent that speaks the [Model Context Protocol](https://modelcontextprotocol.io) read and act on your transactions directly, the same way the command line does. There's nothing to install separately — it's the same `bodger` binary, running in a different mode.

Point your MCP client at the command rather than a network address — most clients ask for a command to run, not a URL:

```json
{
  "mcpServers": {
    "bodger": {
      "command": "bodger",
      "args": ["mcp"]
    }
  }
}
```

(The exact place you paste this depends on your client — check its own documentation for "add an MCP server" or "add a tool.")

### Trying it with sample data

Want to see this working before pointing it at your real ledger? Build the binary and seed a disposable database with realistic sample data instead of using your own:

```sh
make build
BODGER_DB_PATH=/tmp/bodger-dev.db make seed-dev
```

Then point your MCP client at that build, with `BODGER_DB_PATH` set the same way so it reads the sample data instead of your real one. Most clients let you set environment variables alongside the command:

```json
{
  "mcpServers": {
    "bodger": {
      "command": "/path/to/bin/bodger",
      "args": ["mcp"],
      "env": { "BODGER_DB_PATH": "/tmp/bodger-dev.db" }
    }
  }
}
```

If your client is Claude Code itself, its own CLI does this in one line:

```sh
claude mcp add bodger-dev -e BODGER_DB_PATH=/tmp/bodger-dev.db -- /path/to/bin/bodger mcp
```

Once it's connected, ask your assistant something like "what are my account balances" or "list my last 10 transactions" — that calls `get_account_balances` and `list_transactions` from the table below, against the sample data rather than anything real. Delete `/tmp/bodger-dev.db` (and re-run `make seed-dev`) any time you want a clean slate; a scratch database at a different path never touches your real one at the default location.

Once connected, your assistant can call bodger's tools the same way you'd run a command yourself. Tools are grouped into three levels of trust:

- **Look things up** — checking balances, listing transactions, running reports. Always available, and nothing you do this way changes anything.
- **Make changes** — recording a transaction, editing one, and similar. Available by default, and everything done this way is recorded so you can review it later (see "Reviewing what an assistant has done," below).
- **Undo or bulk changes** — deleting something, or anything that affects many transactions at once. Turned off unless you start the server with `--allow-destructive`:

  ```sh
  bodger mcp --allow-destructive
  ```

  Even then, nothing happens on the first request — your assistant gets back a plain description of what it's about to do, along with a one-time confirmation code. Only a second request, carrying that exact code, actually makes the change. If your assistant tries the same request again with an old code, or with anything changed, it's turned down and has to ask again.

### Look-things-up tools

These are always available, need no confirmation, and aren't recorded in `bodger mcp audit` — nothing you look up ever changes anything.

| Tool | What it does |
| --- | --- |
| `get_account_balances` | Every account's balance as of a date, optionally converted into one currency — the same as `bodger balance`. |
| `list_transactions` | Recorded transactions, filtered by account, category, type, date range, currency, amount range, description, or tags — the same as `bodger transactions list`. |
| `get_category_breakdown` | Spending and income grouped by top-level category over a filtered set of transactions — the same as `bodger report category-breakdown`. |
| `get_budget_actuals` | One budget's plan-vs-actual for a single period — the same as `bodger budgets actuals`. |
| `get_budget_history` | One budget's plan-vs-actual repeated over a range of consecutive months — the same as `bodger budgets history`. |
| `list_fx_rates` | The exchange rate between two currencies, from bodger's own stored rates (never a network fetch), optionally converting an amount — the same as `bodger fx rates list`. |

There's also `whoami`, which just reports the identity bodger's MCP server is acting as.

### Make-changes tools

These are available by default (no `--allow-destructive` needed), run immediately with no confirmation step, and are recorded in `bodger mcp audit`.

| Tool | What it does |
| --- | --- |
| `record_outflow` | Record money leaving an account, optionally attributed to a category — the same as `bodger spend`. |
| `record_inflow` | Record money arriving in an account, optionally attributed to a category — the same as `bodger receive`. |
| `record_transfer` | Record a transfer of money from one account to another — the same as `bodger move`. |
| `edit_transaction` | Replace an existing transaction's fields (a full replacement, not a partial patch) — the same as `bodger transactions edit`. |
| `create_budget` | Create a new monthly budget, optionally with an initial batch of category lines — the same as `bodger budgets add`. |
| `update_budget` | Update a budget's name and start date (a full replacement of both) — the same as `bodger budgets update`. |
| `add_budget_line` | Add one new category line to an existing budget — the same as `bodger budgets lines add`. |
| `update_budget_line` | Update an existing budget line's amount and rollover flag — the same as `bodger budgets lines update`. |
| `remove_budget_line` | Remove one line from an existing budget — the same as `bodger budgets lines remove`. |
| `archive_budget` | Archive a budget: it stops appearing in current listings and creation flows, but its history stays fully queryable and archiving is fully reversible — the same as `bodger budgets archive`. |

### Reviewing what an assistant has done

`bodger mcp audit` lists what an assistant has actually changed — every "make a change" or "undo/bulk change" request it made, most recent first. Looking things up isn't listed here, since nothing you look up ever changes anything.

```sh
bodger mcp audit
bodger mcp audit --limit 20
```

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

All commands default to plain-text output; add `--json` to any of them for machine-readable output instead. Every `bodger report` subcommand and `bodger export csv` below also accepts the same `--account`/`--category`/`--type`/`--from`/`--to`/`--filter-currency`/`--amount-min`/`--amount-max`/`--description`/`--tag`/`--tag-mode` filter flags (shortened to `[filters]` in the table); `trends` and `category-trends` ignore `--from`/`--to` except under `--granularity custom`, per the Analytics section above.

| Command | What it does |
| --- | --- |
| `bodger spend <amount> <category> [--account] [--on] [--tag] [--note]` | Record money you spent |
| `bodger receive <amount> <category> [--account] [--on] [--tag] [--note]` | Record money you received |
| `bodger move <amount> --from <account> --to <account> [--to-amount] [--on] [--tag] [--note]` | Move money between two of your accounts — accounts in different currencies are fine, and the implied exchange rate is shown alongside the transfer; add `--to-amount` to state exactly how much arrived instead of reusing `<amount>`'s digits |
| `bodger balance [--on] [--currency] [--policy] [--pinned-date]` | See what every account holds — add `--currency`/`--policy` to convert every balance into one currency, with the rate shown alongside |
| `bodger balance totals --policy [--on] [--currency] [--pinned-date]` | Your overall net balance, plus breakdowns by account type and by currency |
| `bodger balance net-worth --from <date> --to <date> --policy [--currency] [--pinned-date] [--granularity]` | Your net worth over time, one point per week/month/year (or a single custom bucket) across the given range |
| `bodger report category-breakdown --policy [--currency] [--pinned-date] [filters]` | Spending and income, by top-level category |
| `bodger report cash-flow --policy [--currency] [--pinned-date] [--granularity] [filters]` | Inflow vs. outflow, bucketed by period |
| `bodger report trends --policy [--currency] [--pinned-date] [--granularity] [filters]` | The current period vs. the immediately preceding one |
| `bodger report savings-rate --policy [--currency] [--pinned-date] [filters]` | (Income − outflow) / income, over the filter's date range |
| `bodger report top-transactions --policy [--currency] [--pinned-date] [--limit] [filters]` | Your largest transactions in a period, by absolute amount |
| `bodger report average-transaction-size --policy [--currency] [--pinned-date] [filters]` | Mean transaction size, overall and by top-level category |
| `bodger report category-trends --policy [--currency] [--pinned-date] [--granularity] [filters]` | Per-category spending/income trend deltas |
| `bodger export json [--output]` | Download a complete backup of everything you have, as JSON |
| `bodger export csv [--output] [filters]` | Download your transactions as CSV |
| `bodger import upload <file> --account <account> --date-column <col> --description-column <col> --amount-column <col> [--filename] [--format] [--posted-date-column] [--currency-column] [--external-id-column] [--category-column]` | Stage a CSV file for review — nothing is recorded yet |
| `bodger import list` | List every import you've started |
| `bodger import show <import-id>` | Look up one import's status |
| `bodger import records <import-id>` | List an import's staged rows, with any duplicate/transfer flags |
| `bodger import resolve <record-id> --resolution <confirmed_duplicate\|not_duplicate>` | Record your decision on a flagged row |
| `bodger import commit <import-id>` | Write a staged import's cleared rows as real transactions |
| `bodger import rollback <import-id>` | Undo a committed import |
| `bodger restore [file] --yes` | **Overwrites everything you have** with a JSON backup — reads from `file`, or from standard input if omitted; refuses to run without `--yes` |
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
| `bodger mcp [--allow-destructive]` | Start the MCP server for an AI assistant or agent, over stdio |
| `bodger mcp audit [--limit]` | List what an assistant connected over MCP has actually changed |
| `bodger auth set-password` | Set or change your password |
| `bodger auth token create <name> [--expires]` | Create a new API token |
| `bodger auth token list` | List your API tokens |
| `bodger auth token revoke <id>` | Revoke an API token |
| `bodger fx rates fetch [--pair] [--quote] [--from] [--to]` | Fetch and store exchange rates from the configured provider — defaults to every currency pair you actually use, quoted against your reporting currency; add `--quote` to quote `--pair` entries against a different currency, and `--from`/`--to` for a historical backfill |
| `bodger fx rates list --from <currency> --to <currency> --policy <policy> [--transaction-date] [--pinned-date] [--amount]` | Look up the exchange rate between two currencies under a given conversion policy, optionally converting an amount |
| `bodger config reporting-currency get` | See your configured reporting currency |
| `bodger config reporting-currency set <currency>` | Set your reporting currency |
| `bodger budgets list` | List your budgets |
| `bodger budgets show <budget-id>` | Show one budget and its lines |
| `bodger budgets add <name> [--currency] [--starts-on]` | Create a new (empty) budget |
| `bodger budgets update <budget-id> <name> [--starts-on]` | Rename a budget and set its starts-on date together — omitting `--starts-on` resets it to today |
| `bodger budgets archive <budget-id>` | Archive a budget |
| `bodger budgets lines add <budget-id> <category> <amount> [--rollover]` | Add a line to a budget |
| `bodger budgets lines update <budget-id> <line-id> <amount> [--rollover]` | Update a budget line's amount and rollover flag |
| `bodger budgets lines remove <budget-id> <line-id>` | Remove a line from a budget |
| `bodger budgets actuals <budget-id> [--period]` | Actual spend against a budget's lines for a period (defaults to the current month) |
| `bodger budgets history <budget-id> [--period] [--months]` | Actual-vs-budget over several consecutive months (defaults to 6) |

`<account>` and `<category>` accept either the name you gave it (case-insensitive) or its ID. If a name matches more than one of your accounts or categories, `bodger` lists the candidates instead of guessing. `<id>` is a transaction's own ID, which `bodger transactions list` shows in its last column.

---

## In the meantime

- **What is this?** → [`README.md`](../README.md)
- **How does it work?** → [`architecture.md`](architecture.md)
- **What does it store, and what do the words mean?** → [`data-model.md`](data-model.md)
- **How do I build it?** → [`contributing.md`](contributing.md)

The MCP server lands here in the same PR that ships it, per [`contributing.md`](contributing.md) — not in a catch-up pass afterwards.
