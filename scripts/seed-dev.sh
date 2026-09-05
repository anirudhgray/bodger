#!/usr/bin/env bash
#
# scripts/seed-dev.sh - seed a scratch bodger database with realistic
# household data for local development (issue #115): a handful of
# accounts spanning every account type/currency the domain supports, a
# real category tree (parent + child, not flat - relevant to issue
# #106's hierarchy display), and a wide spread of transactions across
# dates, categories, and accounts, so the web UI's TransactionsList
# filters/pagination and Balances show non-trivial numbers.
#
# This shells out to the real, built `bodger` binary's own CLI commands
# (auth set-password, accounts add, categories add, spend/receive/move)
# - the same commands a real user runs - the same approach
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
#   Accounts and categories are additive-but-deduplicated: each is
#   looked up by name first (via `--json` + jq) and only created if
#   missing, so running this script again over a database it already
#   seeded - or one that happens to already have an account/category of
#   the same name - does not fail on a uniqueness conflict or produce
#   duplicates.
#   Transactions are purely additive. Nothing about a spend/receive/move
#   recorded through the CLI carries an external reference this script
#   could dedupe against, so every run appends another full batch of
#   them. Delete BODGER_DB_PATH's file and re-run for a clean slate
#   instead of relying on idempotency there.
#
# Usage:
#   BODGER_DB_PATH=/tmp/bodger-dev.db make seed-dev
#   BODGER_DB_PATH=/tmp/bodger-dev.db ./scripts/seed-dev.sh [flags]
#
# Flags:
#   --bin PATH       path to the built bodger binary (default: bin/bodger)
#   --force          seed even if BODGER_DB_PATH is unset or the default
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

echo "seed-dev: seeding $BODGER_DB_PATH via $BIN"

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

# --- password ----------------------------------------------------------

echo "seed-dev: setting the dev password"
printf '%s\n' "$PASSWORD" | run auth set-password >/dev/null

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
  local args=(accounts add "$name" --type "$type" --currency "$currency" --opening-balance "$opening")
  if [[ -n "$institution" ]]; then
    args+=(--institution "$institution")
  fi
  run "${args[@]}" >/dev/null
  echo "  - $name ($type, $currency) created"
}

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
add_category "Housing" expense ""
add_category "Rent" expense "Housing"
add_category "Utilities" expense "Housing"
add_category "Food" expense ""
add_category "Groceries" expense "Food"
add_category "Dining Out" expense "Food"
add_category "Transportation" expense ""
add_category "Fuel" expense "Transportation"
add_category "Public Transit" expense "Transportation"
add_category "Entertainment" expense ""
add_category "Streaming" expense "Entertainment"
add_category "Hobbies" expense "Entertainment"
add_category "Health" expense ""
add_category "Insurance" expense "Health"
add_category "Pharmacy" expense "Health"
add_category "Salary" income ""
add_category "Freelance" income ""
add_category "Investments" income ""
add_category "Dividends" income "Investments"
add_category "Interest" income "Investments"

# --- transactions ----------------------------------------------------------

echo "seed-dev: recording transactions (additive - see this script's header comment)"

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

echo "seed-dev: done"
