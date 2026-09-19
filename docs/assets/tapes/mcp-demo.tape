# mcp-demo.tape
#
# Records a short demo of `bodger mcp` from the CLI side: the --help
# output (tiering, destructive-tool gating), the server actually running
# briefly over stdio (silent by design -- it only speaks JSON-RPC to a
# connected client, so a few quiet seconds here is expected, not a
# recording bug), and `bodger mcp audit`, which lists write-tier tool
# calls a real MCP client (Claude Desktop, Claude Code, etc.) made
# against this scratch DB.
#
# Renders against the same scratch database as cli-demo.tape:
#   BODGER_DB_PATH=/tmp/bodger-vhs-cli-in.db
# (INR-centric "in" locale, seeded via
#   BODGER_DB_PATH=/tmp/bodger-vhs-cli-in.db ./scripts/seed-dev.sh --locale in)
#
# Run from the repo root:
#   vhs docs/assets/tapes/mcp-demo.tape

Output docs/assets/gifs/mcp-demo.gif

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

Hide
Type "export BODGER_DB_PATH=/tmp/bodger-vhs-cli-in.db"
Enter
Type "export PATH=$PWD/bin:$PATH"
Enter
Type "clear"
Enter
Show

Type "bodger mcp --help"
Enter
Sleep 4s

Hide
Type "clear"
Enter
Show

Type "echo '# starts over stdio -- silent until an MCP client connects'"
Enter
Sleep 800ms

Type "timeout 2 bodger mcp --allow-destructive"
Enter
Sleep 3s

Hide
Type "clear"
Enter
Show

Type "bodger mcp audit"
Enter
Sleep 4s
