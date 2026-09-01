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

If you have more than one account and don't say which with `--account` (for `spend`/`receive`) or `--from`/`--to` (for `move`), `bodger` tells you it can't guess and lists your accounts so you can try again.

A couple of extra flags work on all three:

```sh
bodger spend 450 dining --account Cash --tag "date-night" --tag weekend --note "Anniversary dinner"
```

`--tag` can be repeated. `--note` is free text for anything you want to remember about the entry.

**Recording a split** (one expense across several categories, like a shopping trip that's part groceries and part household goods) isn't built yet — it's planned for the next milestone.

---

## Reading balances

```sh
bodger balance
```

Shows what every account holds right now. Add `--on` to see what it held on some other date:

```sh
bodger balance --on 2026-01-01
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

It binds `127.0.0.1` (loopback) by default, and there's no login yet, so it refuses to start bound to anything else — set `BODGER_HTTP_BIND_ADDR` if you need a different loopback address or port, but it will still refuse a non-loopback one until authentication exists. If you want it reachable from another machine in the meantime, put it behind something that handles authentication itself (a reverse proxy, a VPN) rather than exposing it directly.

Every resource the CLI can touch has an equivalent under `/api/v1`: accounts, categories, transactions, transfers, and balances, plus `/healthz` to check the server is up. A request with no `date` field books to today the same way `spend`/`receive`/`move` do — resolved on the server, in your configured timezone, never by the client. Amounts are always sent and returned as plain decimal strings with a separate currency field, never as numbers.

```sh
curl http://127.0.0.1:8080/api/v1/accounts
curl -X POST http://127.0.0.1:8080/api/v1/transactions \
  -d '{"type":"outflow","account":"HDFC Savings","category":"groceries","amount":"800","description":"Groceries"}'
```

The full set of routes, request and response shapes, and error codes is described in the project's OpenAPI document (`internal/surface/http/openapi.json`) — point any OpenAPI-aware tool at that file for a browsable reference.

---

## Command reference

All commands default to plain-text output; add `--json` to any of them for machine-readable output instead.

| Command | What it does |
| --- | --- |
| `bodger spend <amount> <category> [--account] [--on] [--tag] [--note]` | Record money you spent |
| `bodger receive <amount> <category> [--account] [--on] [--tag] [--note]` | Record money you received |
| `bodger move <amount> --from <account> --to <account> [--on] [--tag] [--note]` | Move money between two of your accounts |
| `bodger balance [--on]` | See what every account holds |
| `bodger accounts list` | List your accounts |
| `bodger accounts add <name> --type <type> [--currency] [--opening-balance] [--opening-balance-date] [--institution] [--sort-order]` | Add an account |
| `bodger accounts archive <account>` | Archive an account |
| `bodger accounts set-opening-balance <account> <amount> [--on]` | Re-declare an account's starting balance |
| `bodger categories list` | List your categories |
| `bodger categories add <name> --type <type> [--parent] [--sort-order]` | Add a category |
| `bodger categories rename <category> <new-name>` | Rename a category |
| `bodger categories archive <category>` | Archive a category |
| `bodger serve` | Start the REST API server |

`<account>` and `<category>` accept either the name you gave it (case-insensitive) or its ID. If a name matches more than one of your accounts or categories, `bodger` lists the candidates instead of guessing.

Editing or deleting a transaction after you've recorded it isn't wired up as a command yet, even though nothing about it is destructive under the hood — it's tracked as follow-up work.

---

## In the meantime

- **What is this?** → [`README.md`](../README.md)
- **How does it work?** → [`architecture.md`](architecture.md)
- **What does it store, and what do the words mean?** → [`data-model.md`](data-model.md)
- **How do I build it?** → [`contributing.md`](contributing.md)

The web UI, multi-currency, reports, budgets, and the MCP server all land here in the same PR that ships them, per [`contributing.md`](contributing.md) — not in a catch-up pass afterwards.
