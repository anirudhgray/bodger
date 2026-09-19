#!/usr/bin/env bash
#
# scripts/seed-dev.sh - seed a scratch bodger database with realistic
# household data for local development (issue #115): a password, a
# couple of API tokens, a handful of accounts spanning every account
# type/currency the domain supports, a real category tree (parent +
# child, not flat - relevant to issue #106's hierarchy display), and a
# wide spread of transactions across dates, categories, and accounts, so
# the web UI's TransactionsList filters/pagination and Balances show
# non-trivial numbers.
#
# Two currency/locale profiles are available via --locale:
#   us (default)  a USD/EUR-centric household - Chase/Ally/Vanguard style
#                 accounts, no budget or recurring rules.
#   in            an INR-centric household - HDFC/ICICI/Groww style
#                 accounts, plus a monthly budget and two recurring rules
#                 (rent, salary), so a fresh contributor who isn't US-based
#                 sees sample data that actually looks like theirs.
# Password/API-token seeding is shared between both - only the accounts,
# categories, transactions, and (for "in") budget/recurring rules differ.
#
# This shells out to the real, built `bodger` binary's own CLI commands
# (auth set-password, auth token create, accounts add, categories add,
# spend/receive/move) - the same commands a real user runs - the same approach
# web/e2e/global-setup.ts takes for its single account/category, scaled
# up into an actual development fixture. It never inserts into the
# database directly, so every seeded row is only ever as valid as what
# the real domain/app layer would accept from a person typing it in.
#
# Safety (never seed fake data into a real ledger by accident):
#   This script refuses to run unless BODGER_DB_PATH is set in the
#   environment to something other than bodger's own production default
#   ("bodger.db", internal/platform/config.Defaults.DBPath) - the exact
#   same env var `bodger serve` and every CLI command already read to
#   pick a database file. That's the simplest check that's still hard to
#   trip by accident: an empty/unset BODGER_DB_PATH, or one explicitly
#   set to "bodger.db", refuses; anything else proceeds. Pass --force to
#   seed the default path anyway (there is no interactive confirmation
#   prompt - --force is the opt-in).
#
# Idempotency:
#   API tokens, accounts, and categories are additive-but-deduplicated:
#   each is looked up by name first (via `--json` + jq) and only created
#   if missing, so running this script again over a database it already
#   seeded - or one that happens to already have a token/account/category
#   of the same name - does not fail on a uniqueness conflict or produce
#   duplicates. A skipped token's plaintext isn't re-shown (bodger itself
#   never shows it again either) - delete the DB and re-run if you need
#   a fresh value.
#   Transactions are purely additive. Nothing about a spend/receive/move
#   recorded through the CLI carries an external reference this script
#   could dedupe against, so every run appends another full batch of
#   them. Delete BODGER_DB_PATH's file and re-run for a clean slate
#   instead of relying on idempotency there.
#
# Usage:
#   BODGER_DB_PATH=/tmp/bodger-dev.db make seed-dev
#   BODGER_DB_PATH=/tmp/bodger-dev.db make seed-dev ARGS="--locale in"
#   BODGER_DB_PATH=/tmp/bodger-dev.db ./scripts/seed-dev.sh [flags]
#
# Flags:
#   --bin PATH       path to the built bodger binary (default: bin/bodger)
#   --force          seed even if BODGER_DB_PATH is unset or the default
#   --locale LOCALE  which currency/locale profile to seed: "us" or "in"
#                    (default: us)
#   --password PASS  password to set (default: dev-seed-password123)
#   -h, --help       print this help and exit
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Must match internal/platform/config.Defaults.DBPath exactly - this is
# the value BODGER_DB_PATH being unset (or set to this) resolves to.
readonly DEFAULT_DB_PATH="bodger.db"

BIN="$REPO_ROOT/bin/bodger"
FORCE=0
LOCALE="us"
PASSWORD="dev-seed-password123"

print_usage() {
  awk '/^#!/{next} /^# ?/{sub(/^# ?/, ""); print; next} {exit}' "$0"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
  --bin)
    BIN="${2:?--bin needs a path}"
    shift 2
    ;;
  --force)
    FORCE=1
    shift
    ;;
  --locale)
    LOCALE="${2:?--locale needs a value: us or in}"
    shift 2
    ;;
  --password)
    PASSWORD="${2:?--password needs a value}"
    shift 2
    ;;
  -h | --help)
    print_usage
    exit 0
    ;;
  *)
    echo "seed-dev: unknown argument: $1" >&2
    print_usage
    exit 1
    ;;
  esac
done

if [[ "$LOCALE" != "us" && "$LOCALE" != "in" ]]; then
  echo "seed-dev: unknown --locale \"$LOCALE\" - must be \"us\" or \"in\"" >&2
  exit 1
fi

DB_PATH="${BODGER_DB_PATH:-}"
if [[ ("$DB_PATH" == "" || "$DB_PATH" == "$DEFAULT_DB_PATH") && "$FORCE" -ne 1 ]]; then
  if [[ "$DB_PATH" == "" ]]; then
    reason="BODGER_DB_PATH is unset"
  else
    reason="BODGER_DB_PATH is set to bodger's own production default (\"$DEFAULT_DB_PATH\")"
  fi
  cat >&2 <<EOF
seed-dev: refusing to run - $reason.

This script records a large batch of fake accounts/categories/transactions
through the real bodger CLI. Point it at a scratch database first:

  BODGER_DB_PATH=/tmp/bodger-dev.db make seed-dev

or pass --force if you really mean to seed the default path.
EOF
  exit 1
fi
export BODGER_DB_PATH="${DB_PATH:-$DEFAULT_DB_PATH}"

if [[ ! -x "$BIN" ]]; then
  echo "seed-dev: no bodger binary at $BIN - run 'make build-bin' (or 'make build') first, or pass --bin PATH" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "seed-dev: this script needs jq, to check for already-seeded accounts/categories - install it and re-run" >&2
  exit 1
fi

echo "seed-dev: seeding $BODGER_DB_PATH via $BIN (locale: $LOCALE)"

run() { "$BIN" "$@"; }

# days_ago prints the date N days before today as YYYY-MM-DD, portable
# across BSD date (macOS) and GNU date (Linux/CI) - their -v/-d flags for
# relative offsets are mutually incompatible, so this probes for which
# one is available exactly once and then always takes that branch.
if date -v-1d >/dev/null 2>&1; then
  days_ago() { date -v-"$1"d +%Y-%m-%d; }
else
  days_ago() { date -d "-$1 days" +%Y-%m-%d; }
fi

# month_day prints the date, YYYY-MM-DD, for day-of-month $2, $1 calendar
# months before the current month (0 = this month) - same BSD-vs-GNU
# probe-once-then-branch approach as days_ago above. The "in" locale's
# transactions are specified as fixed pay-cycle-style dates (rent on the
# 2nd, salary on the 1st, ...) rather than an offset from today, so they
# need month-and-day arithmetic instead of a plain day count.
if date -v-1m >/dev/null 2>&1; then
  month_day() { date -v-"$1"m -v"$2"d +%Y-%m-%d; }
else
  month_day() { printf '%s-%02d\n' "$(date -d "-$1 month" +%Y-%m)" "$2"; }
fi

# run_dated runs a bodger command with `--on` appended for month_day's
# date, but skips it entirely (rather than failing) when that date is
# still in the future - so, e.g., a current-month entry later than
# today's day-of-month doesn't emit a future-dated transaction just
# because this script happened to run early in the month.
today="$(date +%Y-%m-%d)"
run_dated() {
  local offset="$1" day="$2"
  shift 2
  local on
  on="$(month_day "$offset" "$day")"
  if [[ "$on" > "$today" ]]; then
    return 0
  fi
  run "$@" --on "$on" >/dev/null
}

# --- password ----------------------------------------------------------

echo "seed-dev: setting the dev password"
printf '%s\n' "$PASSWORD" | run auth set-password >/dev/null

# --- api tokens ----------------------------------------------------------

echo "seed-dev: seeding API tokens"
existing_tokens="$(run auth token list --json | jq -r '.data[].name')"

token_exists() { grep -Fxq "$1" <<<"$existing_tokens"; }

add_token() {
  local name="$1"
  if token_exists "$name"; then
    echo "  - $name (already exists, skipping)"
    return
  fi
  local plaintext
  plaintext="$(run auth token create "$name" --json | jq -r '.data.token')"
  echo "  - $name created: $plaintext"
}

# Two tokens, so the web UI's own token-management screen (issue #57) has
# more than one row to show. The plaintext is only ever shown once - by
# bodger itself, and here, since a dev fixture is exactly the case where
# printing it to the terminal for you to copy is the point.
add_token "Dev CLI"
add_token "Dev Web"

# --- accounts ------------------------------------------------------------

echo "seed-dev: seeding accounts"
existing_accounts="$(run accounts list --json | jq -r '.data[].name')"

account_exists() { grep -Fxq "$1" <<<"$existing_accounts"; }

add_account() {
  local name="$1" type="$2" currency="$3" opening="$4" institution="$5"
  if account_exists "$name"; then
    echo "  - $name (already exists, skipping)"
    return
  fi
  local args=(accounts add "$name" --type "$type" --currency "$currency")
  if [[ -n "$opening" ]]; then
    args+=(--opening-balance "$opening")
  fi
  if [[ -n "$institution" ]]; then
    args+=(--institution "$institution")
  fi
  run "${args[@]}" >/dev/null
  echo "  - $name ($type, $currency) created"
}

if [[ "$LOCALE" == "us" ]]; then
  # Spans every account kind ledger.AccountKind defines (bank, cash,
  # credit_card, wallet, investment, loan, other) and two currencies, so
  # the currency/type pickers in the web UI have real variety to show.
  add_account "Chase Checking" bank USD 5000.00 "Chase"
  add_account "Ally Savings" bank USD 15000.00 "Ally Bank"
  add_account "Everyday Wallet" wallet USD 200.00 ""
  add_account "Cash Envelope" cash USD 250.00 ""
  add_account "Chase Sapphire" credit_card USD -450.00 "Chase"
  add_account "Vanguard Brokerage" investment USD 25000.00 "Vanguard"
  add_account "Federal Student Loan" loan USD -12000.00 "Federal Student Aid"
  add_account "Revolut EUR" bank EUR 800.00 "Revolut"
  add_account "Home Safe" other USD 200.00 ""
else
  # An India-based household: INR for the everyday accounts, plus a
  # couple of foreign-currency ones (USD, EUR) for multi-currency flavor
  # - same spirit as the "us" profile's own Revolut EUR account, just
  # more of the dataset lives in INR instead of USD.
  add_account "HDFC Savings" bank INR 150000.00 "HDFC Bank"
  add_account "ICICI Salary Account" bank INR 45000.00 "ICICI Bank"
  add_account "Cash Wallet" cash INR 3200.00 ""
  add_account "HDFC Credit Card" credit_card INR "" "HDFC Bank"
  add_account "Groww Investments" investment INR 380000.00 "Groww"
  add_account "Wise USD" bank USD 1200.00 "Wise"
  add_account "Revolut EUR" bank EUR 400.00 "Revolut"
fi

# --- categories ----------------------------------------------------------

echo "seed-dev: seeding categories"
existing_categories="$(run categories list --json | jq -r '.data[].name')"

category_exists() { grep -Fxq "$1" <<<"$existing_categories"; }

add_category() {
  local name="$1" type="$2" parent="$3"
  if category_exists "$name"; then
    echo "  - $name (already exists, skipping)"
    return
  fi
  local args=(categories add "$name" --type "$type")
  if [[ -n "$parent" ]]; then
    args+=(--parent "$parent")
  fi
  run "${args[@]}" >/dev/null
  echo "  - $name${parent:+ (under $parent)} created"
}

# A real two-level tree, not a flat list (issue #106): eight top-level
# categories, six of them with children. Every name here is globally
# unique across the whole tree on purpose - normalize.Ref (internal/app)
# resolves a --category/--parent reference by exact name first, and this
# script always passes an exact name, so there is no ambiguity to hit.
# Both locales share the same top-level shape; only two leaves differ
# (Public Transit/Hobbies for "us" vs Metro/Outings for "in").
add_category "Housing" expense ""
add_category "Rent" expense "Housing"
add_category "Utilities" expense "Housing"
add_category "Food" expense ""
add_category "Groceries" expense "Food"
add_category "Dining Out" expense "Food"
add_category "Transportation" expense ""
add_category "Fuel" expense "Transportation"
add_category "Entertainment" expense ""
add_category "Streaming" expense "Entertainment"
add_category "Health" expense ""
add_category "Insurance" expense "Health"
add_category "Pharmacy" expense "Health"
add_category "Salary" income ""
add_category "Freelance" income ""
add_category "Investments" income ""
add_category "Dividends" income "Investments"
add_category "Interest" income "Investments"

if [[ "$LOCALE" == "us" ]]; then
  add_category "Public Transit" expense "Transportation"
  add_category "Hobbies" expense "Entertainment"
else
  add_category "Metro" expense "Transportation"
  add_category "Outings" expense "Entertainment"
fi

# --- transactions ----------------------------------------------------------

echo "seed-dev: recording transactions (additive - see this script's header comment)"

if [[ "$LOCALE" == "us" ]]; then

  # Spend: cycles through every expense leaf category, three days apart,
  # reaching back about seven months - enough rows and enough date spread
  # to page through and filter by date in the web UI. Each category has a
  # primary account it normally posts to (big recurring bills go to the
  # main checking account or the credit card, the way a real household's
  # do) and a fallback used one week in four for variety - Everyday Wallet
  # and Cash Envelope only ever appear as a small category's primary, so
  # neither account's modest opening balance gets driven deeply negative
  # by a bill sized for a bank account.
  expense_cats=(Rent Utilities Groceries "Dining Out" Fuel "Public Transit" Streaming Hobbies Insurance Pharmacy)
  expense_base=(1200 90 60 35 45 25 15 20 110 18)
  expense_primary=("Chase Checking" "Chase Checking" "Chase Checking" "Chase Sapphire" "Chase Checking" "Everyday Wallet" "Chase Sapphire" "Chase Sapphire" "Chase Checking" "Cash Envelope")
  expense_fallback=("Chase Sapphire" "Chase Sapphire" "Chase Sapphire" "Chase Checking" "Chase Sapphire" "Chase Checking" "Chase Checking" "Chase Checking" "Chase Sapphire" "Chase Checking")
  n_cats=${#expense_cats[@]}
  for i in $(seq 0 69); do
    idx=$((i % n_cats))
    cat="${expense_cats[$idx]}"
    base="${expense_base[$idx]}"
    cents=$(((i * 37) % 100))
    extra=$((i % 15))
    amount=$(printf "%d.%02d" $((base + extra)) "$cents")
    if ((i % 4 == 3)); then
      acct="${expense_fallback[$idx]}"
    else
      acct="${expense_primary[$idx]}"
    fi
    on="$(days_ago $((i * 3)))"
    run spend "$amount" "$cat" --account "$acct" --on "$on" >/dev/null
  done
  echo "  - 70 spend transactions (USD)"

  # A handful of spends on the EUR account too, so currency variety shows
  # up in TransactionsList/Balances, not just in the accounts list.
  eur_cats=("Dining Out" Groceries Streaming Hobbies "Public Transit")
  for i in $(seq 0 9); do
    cat="${eur_cats[$((i % ${#eur_cats[@]}))]}"
    amount=$(printf "%d.%02d" $((15 + i)) $(((i * 17) % 100)))
    on="$(days_ago $((i * 12 + 2)))"
    run spend "$amount" "$cat" --account "Revolut EUR" --on "$on" >/dev/null
  done
  echo "  - 10 spend transactions (EUR)"

  # Receive: biweekly salary, a few irregular freelance payments, quarterly
  # dividends, monthly interest.
  for i in $(seq 0 14); do
    on="$(days_ago $((i * 14)))"
    run receive 3200.00 Salary --account "Chase Checking" --on "$on" >/dev/null
  done
  echo "  - 15 salary payments"

  for i in $(seq 0 4); do
    amount=$(printf "%d.%02d" $((300 + i * 120)) $(((i * 29) % 100)))
    on="$(days_ago $((i * 40 + 10)))"
    run receive "$amount" Freelance --account "Chase Checking" --on "$on" >/dev/null
  done
  echo "  - 5 freelance payments"

  for i in $(seq 0 2); do
    amount=$(printf "%d.%02d" $((150 + i * 60)) $(((i * 41) % 100)))
    on="$(days_ago $((i * 60 + 20)))"
    run receive "$amount" Dividends --account "Vanguard Brokerage" --on "$on" >/dev/null
  done
  echo "  - 3 dividend payments"

  for i in $(seq 0 6); do
    amount=$(printf "%d.%02d" $((20 + i * 4)) $(((i * 23) % 100)))
    on="$(days_ago $((i * 30 + 5)))"
    run receive "$amount" Interest --account "Ally Savings" --on "$on" >/dev/null
  done
  echo "  - 7 interest payments"

  # Move: a monthly savings transfer, occasional credit-card payments and
  # cash withdrawals - all between USD accounts (transfers between
  # different currencies aren't part of this issue's scope).
  for i in $(seq 0 6); do
    on="$(days_ago $((i * 30 + 3)))"
    run move 500.00 --from "Chase Checking" --to "Ally Savings" --on "$on" >/dev/null
  done
  echo "  - 7 transfers to savings"

  for i in $(seq 0 3); do
    amount=$(printf "%d.%02d" $((150 + i * 20)) $(((i * 31) % 100)))
    on="$(days_ago $((i * 45 + 8)))"
    run move "$amount" --from "Chase Checking" --to "Chase Sapphire" --on "$on" >/dev/null
  done
  echo "  - 4 credit card payments"

  for i in $(seq 0 3); do
    on="$(days_ago $((i * 35 + 15)))"
    run move 100.00 --from "Chase Checking" --to "Everyday Wallet" --on "$on" >/dev/null
  done
  echo "  - 4 cash withdrawals"

else

  # An India-based household, spread across the last three calendar
  # months (via month_day/run_dated above) rather than a fixed offset
  # from today, so the data always reads as "recent" no matter when this
  # script runs. Offsets are "months before the current month": 2 months
  # ago, 1 month ago, this month.
  utilities_amounts=(3150.00 3200.00 3280.00)
  for i in 0 1 2; do
    offset=$((2 - i))
    run_dated "$offset" 1 receive 150000.00 Salary --account "ICICI Salary Account"
    run_dated "$offset" 2 spend 28000.00 Rent --account "ICICI Salary Account"
    run_dated "$offset" 5 spend "${utilities_amounts[$i]}" Utilities --account "ICICI Salary Account"
    run_dated "$offset" 3 spend 499.00 Streaming --account "HDFC Credit Card"
    run_dated "$offset" 3 move 12000.00 --from "ICICI Salary Account" --to "HDFC Savings"
  done
  echo "  - 3 months of salary/rent/utilities/streaming/savings-transfer (INR)"

  # Weekly-ish groceries and dining out, several times a month each -
  # dining out alternates Cash Wallet / HDFC Credit Card for variety.
  grocery_days=(4 9 14 19)
  grocery_amounts=(1900.00 2450.00 2800.00 3350.00)
  dining_days=(2 8 13 17)
  dining_amounts=(950.00 1450.00 1900.00 2600.00)
  dining_accounts=("Cash Wallet" "HDFC Credit Card" "Cash Wallet" "HDFC Credit Card")
  for i in 0 1 2; do
    offset=$((2 - i))
    for j in 0 1 2 3; do
      run_dated "$offset" "${grocery_days[$j]}" spend "${grocery_amounts[$j]}" Groceries --account "HDFC Savings"
      run_dated "$offset" "${dining_days[$j]}" spend "${dining_amounts[$j]}" "Dining Out" --account "${dining_accounts[$j]}"
    done
  done
  echo "  - 12 grocery spends and 12 dining out spends (INR)"

  # Fuel twice a month, metro once a month - both cheap enough not to
  # matter which account, so they stay on their obvious defaults.
  fuel_days=(6 17)
  fuel_amounts=(1780.00 1950.00)
  metro_amounts=(410.00 445.00 470.00)
  for i in 0 1 2; do
    offset=$((2 - i))
    for j in 0 1; do
      run_dated "$offset" "${fuel_days[$j]}" spend "${fuel_amounts[$j]}" Fuel --account "HDFC Savings"
    done
    run_dated "$offset" 10 spend "${metro_amounts[$i]}" Metro --account "Cash Wallet"
  done
  echo "  - 6 fuel spends and 3 metro spends (INR)"

  # Health: one insurance premium, a couple of pharmacy runs.
  run_dated 1 12 spend 5200.00 Insurance --account "HDFC Savings"
  run_dated 2 15 spend 650.00 Pharmacy --account "HDFC Savings"
  run_dated 0 8 spend 950.00 Pharmacy --account "HDFC Savings"
  echo "  - 1 insurance payment and 2 pharmacy spends (INR)"

  # Outings, on the credit card, a few times.
  run_dated 2 20 spend 1050.00 Outings --account "HDFC Credit Card"
  run_dated 1 22 spend 1250.00 Outings --account "HDFC Credit Card"
  run_dated 0 16 spend 980.00 Outings --account "HDFC Credit Card"
  echo "  - 3 outings spends (INR)"

  # Income variety: one freelance receipt a month, dividends in two of
  # the three months, interest every month.
  freelance_days=(25 23 15)
  freelance_amounts=(21000.00 24500.00 19000.00)
  interest_days=(30 30 18)
  interest_amounts=(600.00 605.00 592.00)
  for i in 0 1 2; do
    offset=$((2 - i))
    run_dated "$offset" "${freelance_days[$i]}" receive "${freelance_amounts[$i]}" Freelance --account "HDFC Savings"
    run_dated "$offset" "${interest_days[$i]}" receive "${interest_amounts[$i]}" Interest --account "HDFC Savings"
  done
  run_dated 2 28 receive 1550.00 Dividends --account "Groww Investments"
  run_dated 1 27 receive 1480.00 Dividends --account "Groww Investments"
  echo "  - 3 freelance payments, 2 dividend payments, 3 interest payments (INR)"

  # Move: investing a chunk of savings, paying off the credit card, and
  # one early cash top-up so Cash Wallet doesn't go negative against its
  # own dining out/metro spends above.
  run_dated 2 31 move 20000.00 --from "HDFC Savings" --to "Groww Investments"
  run_dated 1 31 move 20000.00 --from "HDFC Savings" --to "Groww Investments"
  run_dated 2 28 move 15000.00 --from "HDFC Savings" --to "HDFC Credit Card"
  run_dated 1 28 move 18000.00 --from "HDFC Savings" --to "HDFC Credit Card"
  run_dated 2 2 move 10000.00 --from "HDFC Savings" --to "Cash Wallet"
  echo "  - 2 investment transfers, 2 credit card payments, 1 cash top-up (INR)"

  # A few foreign-currency spends for multi-currency flavor, same idea as
  # the "us" locale's own EUR spends above.
  run_dated 2 12 spend 45.00 "Dining Out" --account "Wise USD"
  run_dated 1 14 spend 120.00 "Dining Out" --account "Wise USD"
  run_dated 2 18 spend 35.00 Groceries --account "Revolut EUR"
  run_dated 1 19 spend 60.00 Outings --account "Revolut EUR"
  echo "  - 2 spends (USD) and 2 spends (EUR)"

  # --- budget --------------------------------------------------------------

  echo "seed-dev: seeding budget"
  existing_budgets="$(run budgets list --json | jq -r '.data[].name')"
  if grep -Fxq "Monthly Budget" <<<"$existing_budgets"; then
    echo "  - Monthly Budget (already exists, skipping)"
  else
    budget_id="$(run budgets add "Monthly Budget" --currency INR --starts-on "$(month_day 1 1)" --json | jq -r '.data.id')"
    run budgets lines add "$budget_id" Groceries 12000 >/dev/null
    run budgets lines add "$budget_id" "Dining Out" 8000 >/dev/null
    run budgets lines add "$budget_id" Fuel 4000 >/dev/null
    run budgets lines add "$budget_id" Streaming 500 >/dev/null
    echo "  - Monthly Budget created (4 lines)"
  fi

  # --- recurring rules -------------------------------------------------------

  echo "seed-dev: seeding recurring rules"
  existing_rules="$(run recurring list --json | jq -r '.data[].description')"
  if grep -Fxq "Rent" <<<"$existing_rules"; then
    echo "  - Rent (already exists, skipping)"
  else
    run recurring create "ICICI Salary Account" Rent 28000.00 "Rent" --frequency monthly --day-of-month 2 --starts-on "$(month_day 1 1)" >/dev/null
    echo "  - Rent recurring rule created"
  fi
  if grep -Fxq "Salary" <<<"$existing_rules"; then
    echo "  - Salary (already exists, skipping)"
  else
    run recurring create "ICICI Salary Account" Salary 150000.00 "Salary" --frequency monthly --day-of-month 1 --starts-on "$(month_day 1 1)" >/dev/null
    echo "  - Salary recurring rule created"
  fi
  run recurring refresh >/dev/null
  echo "  - recurring occurrences refreshed"

fi

echo "seed-dev: done"
