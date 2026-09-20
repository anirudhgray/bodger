# cli-demo.tape
#
# Records a short, polished walkthrough of core `bodger` CLI usage for
# the README / user guide. Renders against a scratch database seeded by:
#
#   BODGER_DB_PATH=/tmp/bodger-vhs-cli-in.db ./scripts/seed-dev.sh --locale in
#
# which already includes a "Monthly Budget" (Rent/Fuel/Insurance/etc.
# lines) and two recurring rules (monthly rent, monthly salary) --
# the INR-centric locale profile, so screenshots/GIFs read as an
# India-based household (HDFC/ICICI/Groww accounts) rather than the
# US/EUR-centric default.
#
# The closing segment runs `bodger import suggest` (issue #306) against a
# small statement staged silently in the hidden setup block above (so the
# recording doesn't spend a segment on the unrelated `import upload`
# flow). BODGER_TYPESAFE_API_KEY is deliberately never set anywhere in
# this tape, so this always records the "not configured" state -- the
# one state of that command that's honestly re-renderable in a
# checked-in asset: it needs no key and spends no credits, unlike a real
# "suggestions present" recording, which would need a live typesafe.ai
# key and would spend real credits on every re-render.
#
# Run from the repo root:
#   vhs docs/assets/tapes/cli-demo.tape

Output docs/assets/gifs/cli-demo.gif

Require bash

Set Shell "bash"
Set FontSize 16
Set Width 1400
Set Height 800
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 35ms
Set WindowBar Colorful
Set WindowBarSize 40
Set BorderRadius 10
Set Margin 30
Set MarginFill "#1e1e2e"
Set Framerate 24

# --- silent setup: point at the scratch DB and put the built binary on
# PATH, so the rest of the recording reads as a normal `bodger ...`
# session rather than `./bin/bodger ...` with env vars visible.
Hide
Type "export BODGER_DB_PATH=/tmp/bodger-vhs-cli-in.db"
Enter
Type "export PATH=$PWD/bin:$PATH"
Enter
Type "printf 'Date,Description,Amount\n2026-09-10,BigBasket Groceries,-1450.00\n2026-09-12,Swiggy Order,-620.00\n' > /tmp/bodger-vhs-statement.csv"
Enter
Type "IMPORT_ID=$(bodger import upload /tmp/bodger-vhs-statement.csv --account 'HDFC Savings' --date-column Date --description-column Description --amount-column Amount --json | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)"
Enter
Type "clear"
Enter
Show

Type "bodger spend 850.00 Groceries --account 'HDFC Savings' --note 'Weekly grocery run'"
Enter
Sleep 2s

Type "bodger receive 12000.00 Freelance --account 'HDFC Savings' --note 'Logo design gig'"
Enter
Sleep 2s

Type "bodger move 5000.00 --from 'HDFC Savings' --to 'Groww Investments' --note 'Monthly SIP top-up'"
Enter
Sleep 2s

Type "bodger balance"
Enter
Sleep 3s

Type "bodger transactions list --limit 5"
Enter
Sleep 3s

Type "bodger report cash-flow --policy current --from 2026-07-01 --to 2026-09-19 --filter-currency INR --currency INR"
Enter
Sleep 3s

Type "bodger recurring list"
Enter
Sleep 3s

Type "bodger import suggest $IMPORT_ID"
Enter
Sleep 3s
