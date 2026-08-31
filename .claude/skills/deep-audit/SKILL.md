---
name: deep-audit
description: Run a fresh-context, technique-driven correctness/quality audit over a given scope (a package, a boundary between two surfaces, or the whole codebase) - distinct from a diff review. Produces an agent-consumable plan artefact for a separate execution session, plus a human-facing summary. Manually invoked only: /deep-audit <scope>.
disable-model-invocation: true
---

# Deep audit

This is not `/code-review`. `/code-review` is a diff review - scoped to what
changed. This skill is for a standing, fresh-context sweep of code that
_hasn't_ changed recently, looking for correctness gaps and cross-package
inconsistencies that per-PR review structurally cannot see. Worth adding
once the codebase has enough surface area that these findings stop being
visible to any single PR's diff - on the project this was templated from,
one audit run found a fourth exhaustive switch over a domain enum missing a
case, most error-surfacing sites in a UI layer silently not logging, and an
83x unit-conversion error, none of which any implementing PR's own
build/test run had caught.

Scope for this run: **$ARGUMENTS** - a package, a specific
boundary/invariant, or "whole codebase" if unspecified.

## What doesn't work

"Read each package carefully" is the lowest-yield move available and isn't
worth this skill's overhead - that's what `/code-review` at a high effort
level already does. Don't reproduce a package checklist here; a set of
subagents each given exactly that brief tends to produce the weakest
findings of a session.

## The five techniques that actually find things

Everything below is repo-agnostic and none of it duplicates `/code-review`.
Apply these to the scope given, not as a rota to complete but as lenses -
some will fit the scope better than others.

1. **Write throwaway executable probes - don't just read.** Reading
   suggests a hypothesis; running it makes the bug undeniable and gives you
   an exact reproduction to hand off. Write a scratch test for anything you
   suspect rather than reasoning about it from the source alone. Delete the
   probe before finishing, but keep its exact inputs/outputs in the
   writeup, and tell the execution phase to recreate it as a real
   regression test - the probe itself isn't the deliverable, the finding
   and its reproduction are.

2. **Differential-test surfaces that do the same thing.** Anywhere two
   entry points implement the same operation (two UI surfaces, two
   repository implementations, two serialization paths), diff their
   behavior on the same input rather than reading each in isolation. This
   angle is disproportionately effective - it can find several separate
   findings from one technique: cases where one surface silently accepts
   what the other would reject or normalize differently.

3. **Treat every doc claim as a testable assertion.** Not "does this doc
   look current" but "go execute the exact thing it promises and check the
   result." This is what catches a doc's own stated guarantee being broken,
   a documented structure that doesn't actually exist, or a report/output
   stating a false reason for its own fallback behavior. Pull claims from
   the architecture doc, the data-model doc, ADRs, and any package-level
   doc comments - anything phrased as a guarantee or invariant is a
   candidate.

4. **Hunt for the second switch.** Find every exhaustive switch/match over
   the same domain enum across the codebase and diff their case lists
   against each other. If one package's switch was recently extended for a
   new case, every other switch over that same type is a suspect until
   checked. This is a search you can often do mechanically (grep for the
   type name, find every `switch`/`case` site) rather than one requiring
   deep reading.

5. **Read test coverage as a map, not a score.** A coverage percentage is
   nearly useless; a coverage _gap_ on code whose own doc comment or naming
   calls it load-bearing is a real finding. Look specifically for
   untested blocks inside anything described as a safety net, fallback, or
   correctness guarantee.

## Process notes

- **Verify subagent claims before including them.** A subagent's report is
  a claim, not a finding - re-run at least the most severe ones yourself
  before they go in the artefact. Findings have turned out to be off by
  orders of magnitude once independently re-run.
- **Budget for fan-out failures.** Subagents can die mid-run (rate limits,
  crashes) and produce nothing recoverable if they were only going to
  report at the end. Have each one write findings to a file incrementally
  as it goes, not only in a final summary.
- **Write the artefact for someone with zero context.** The forcing
  function that makes the plan actually executable later is writing it as
  if the reader has never seen this codebase today - exact file/function,
  the reproduction, the exact fix, or (for anything too large/ambiguous to
  fix inline) a draft issue body. This is also what surfaces judgment
  calls (e.g. "is a naive test that merely passes actually proof the bug
  is fixed, or does it need to fail against the _old_ code first?") that
  are much cheaper to resolve now than for the execution phase to
  re-derive.

## Output

Two artefacts, per this repo's usual fix-inline-vs-file-an-issue rule (see
`orchestrate`'s step 6):

1. **A design-doc plan** in `agents/design-docs/` - every finding, its
   reproduction, and a triage: fix inline (small, low-risk, exact
   file/change) vs. follow-up issue (draft title + body, citing the
   specific doc/invariant it violates). Group inline fixes so an execution
   session can hand bounded slices to worktree-isolated agents without
   re-deriving scope. This design-doc is cleaned up once its contents are
   formalized into issues/docs/PRs, per CLAUDE.md's design-docs
   convention - it isn't a permanent artefact.
2. **A human-facing summary** (post as a comment on whatever issue tracks
   this audit, or hand directly to the developer) - what was found, what's
   being fixed inline vs. filed, and any genuinely ambiguous/
   clarification-needed items for the developer to answer directly, since
   the execution phase shouldn't have to re-derive judgment calls this
   session already made.

Do not execute the plan in this same session/context - this skill stops at
a written plan. Execution (applying inline fixes, filing issues, tagging
them) is a separate pass, ideally via `orchestrate` in a new session so it
gets a full context budget rather than inheriting this audit's.
