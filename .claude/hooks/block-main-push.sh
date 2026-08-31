#!/bin/bash
# block-main-push.sh - Claude Code PreToolUse hook, matcher: Bash.
#
# This project requires every change to main to go through a reviewed PR
# (see CLAUDE.md). This inspects every Bash tool call for a `git push`
# and, if found, dry-runs it to check whether the resulting ref update
# would touch origin/main - denying the call if so.
#
# A client-side hook, not a GitHub branch protection rule: if the repo is
# private on a plan without branch protection, this is the available
# substitute. Prefer a real branch-protection rule instead once that's an
# option - this hook then becomes a convenience/backstop rather than the
# only guard. (See also .githooks/pre-push, which does the same job for
# pushes run outside Claude Code.)
#
# Performance note: this hook fires on *every* Bash tool call, most of
# which have nothing to do with git at all. jq is needed to safely pull
# just the command string out of the tool_input JSON (rather than
# grepping the raw JSON, which could false-positive on a "push" appearing
# inside an unrelated argument or file path), but jq's own process
# startup is a real cost paid on every single Bash call in a session if
# run unconditionally. The cheap substring check below runs first and is
# a strict superset of the real `git push` check further down (anything
# matching that regex necessarily contains "push"), so it can only ever
# skip jq when there is genuinely nothing to act on - never on a real
# push.

input=$(cat)

# Fast path: skip the jq parse (and everything after it) entirely unless
# the raw payload even mentions "push" - case-insensitive, deliberately
# looser than the real check below, so this can never cause a false
# negative.
if ! printf '%s' "$input" | grep -qi 'push'; then
  exit 0
fi

cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // empty')

# Only act on commands that mention a git push at all.
if ! printf '%s' "$cmd" | grep -qE 'git[[:space:]]+push'; then
  exit 0
fi

# Split into logical segments on &&, ||, ;, |, and backtick (POSIX ERE
# alternation prefers the longest match, so "&&"/"||" win over a lone
# "&"/"|" at the same position -- a lone "&" as in "2>&1" is never
# touched since it's not in the alternation at all).
segments=$(printf '%s' "$cmd" | sed -E 's/(\&\&|\|\||;|`)/\
/g')
push_seg=$(printf '%s' "$segments" | grep -m1 -E 'git[[:space:]]+push')
# Trim a trailing redirect and whitespace.
push_seg=$(printf '%s' "$push_seg" | sed -E 's/[[:space:]]*(2>&1|>\/dev\/null|2>\/dev\/null)[[:space:]]*$//')
push_seg=$(printf '%s' "$push_seg" | sed -E 's/[[:space:]]+$//')

if [ -z "$push_seg" ]; then
  echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"Could not safely verify this git push does not target main (this project never allows direct pushes to main). Push manually outside Claude Code if this is intentional."}}'
  exit 0
fi

dry_run_output=$($push_seg --dry-run 2>&1)

if printf '%s' "$dry_run_output" | grep -qE -- '(->[[:space:]]*main([[:space:]]|$))|(\[deleted\][[:space:]]+main\b)'; then
  evidence=$(printf '%s' "$dry_run_output" | grep -E -- '(-> *main( |$))|(\[deleted\] +main\b)' | head -3 | tr '\n' ' ' | sed "s/\"/'/g")
  reason="BLOCKED: this push resolves to origin/main (this project requires all changes to main go through a reviewed PR - see CLAUDE.md). Dry-run showed: ${evidence}. If this is genuinely intentional, push manually outside Claude Code."
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"%s"}}' "$reason"
  exit 0
fi

exit 0
