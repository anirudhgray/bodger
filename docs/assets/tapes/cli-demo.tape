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
