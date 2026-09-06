---
name: orchestrate
description: "Sync bodger's project state (architecture doc status, open GitHub issues, open PRs, recent git log) and pick up the next unit of milestone work -- named issue(s)/a focus area passed as args, or the next unblocked item if none given. Decides sequential vs parallel execution, spawns worktree-isolated agents for bounded self-contained work, integrates the result (merging main in before calling anything ready), knows when newly-discovered work should become its own GitHub issue versus just getting fixed inline, and reports back. Use at the start of a work session on this repo."
---

# Orchestrate

<!--
  Filled in during the M0 architecture phase, from docs/architecture.md
  and the ADRs. The hotspot list in step 3 is the part most likely to go
  stale: it was derived from the planned package layout before any code
  existed, so revisit it once the first few M1 PRs have landed and the
  repo's actual collision points are visible rather than predicted.
-->

This is bodger's work-cycle playbook: how a session picks up where the
project left off, decides what to build and in what order, and how it hands
work to agents versus doing it directly. It assumes CLAUDE.md's baseline
conventions (feature branches, atomic conventional commits, no direct
commits to main, conventional-commit PR titles since PRs are squash-merged,
no Claude co-author trailer, and docs updated in the same PR as the code
they describe) - this doc doesn't repeat those, it's the layer on top: what
to work on and how to sequence it.

## 1. Sync state first

Don't trust anything from a prior conversation's memory - the source of
truth is external. Every orchestrate run starts here:

- `docs/architecture.md` - its Status section (§9), whichever
  milestone/status section is current (check the doc's own headings; don't
  assume it still says an earlier milestone name - the doc is kept current
  per-PR, so trust its actual current structure over any specific section
  name).
- **Native GitHub Milestones**, titled `M<n> <Name> - <scope>` — e.g.
  `M1 Arda - Ledger core, CLI, and REST API` — matching
  `docs/architecture.md` §8. See step 6 for the naming convention. `gh api repos/anirudhgray/bodger/milestones`
  for the current milestone's issue counts, `gh issue list --milestone
  "<title>"` for its actual issues, and `gh label list` for the rest of the
  label scheme (`area:*` for the layer a slice touches, plus `deferred` and
  `blocked`) - don't assume any specific set of labels still exists by the
  time you read this. Read the bodies of candidate issues,
  not just titles - they should cite the docs/ADRs that frame the work and
  often note dependencies on other issues. If the current milestone's open
  issue count is zero, that's a milestone-transition moment - flag it
  prominently in the report (see step 6). Don't treat it as silent
  completion.
- `gh pr list` - anything already in flight; don't duplicate it.
- `git log --oneline -20` on `main` - recent history, in case docs/issues
  are stale relative to what's actually merged.
- `git worktree list` - reconcile against the above. A worktree whose
  branch has already landed on `main` (squash-merged, so its tip commit
  won't show up via `git branch --merged`; confirm by checking the PR's
  merge status or that the branch's content is on `main`) is stale:
  `git worktree remove <path>` (unlock first if needed) and
  `git branch -d <branch>` for the now-fully-merged local branch. Do this
  reconciliation whenever a PR merges, not just at the start of a session.

## 2. Decide what to work on

- If invoked with args (an issue number, a list of issues, or a described
  focus area), that's the scope for this session.
- If invoked bare, pick the next unblocked item(s): prefer whatever's
  attached to the current open milestone over deferred/future-labeled
  work, and within either, skip anything whose dependency note says it's
  waiting on something not yet merged.

## 3. Decide sequential vs. parallel

Two or more candidate items can run in parallel (separate worktree-isolated
agents, launched in one message) only if they're independent in scope AND
low-risk on shared files. bodger's known hotspots, touched by nearly every
feature slice:

| Hotspot | Why it collides |
| --- | --- |
| `internal/app/service.go` | The service container. Grows a field and a constructor parameter per feature - two parallel slices both add one, in the same place |
| `internal/adapters/sqlite/migrations/` | Sequential numeric prefixes (ADR-0007), deliberately - two branches both claim `00007_`. A visible collision, but a guaranteed one |
| `internal/surface/http/router.go` | Central route registry |
| `internal/surface/cli/root.go` | Central cobra command registry |
| `internal/surface/mcp/tools.go` | Central MCP tool registry |
| `internal/surface/conformance/cases_test.go` | The one conformance table (ADR-0005). **Every** user-facing operation adds a row here, so almost every feature slice touches it |
| `internal/domain/ledger/` | Core types. Two slices extending `Transaction` or `Posting` collide |
| `internal/platform/errs/` | The error registry (ADR-0011). Nearly every slice adds a code |
| `docs/architecture.md` §9 Status | Every milestone-item PR edits it, per step 7 |
| `go.mod` / `go.sum` | Any slice adding a dependency |
| `web/src/lib/api.ts`, `web/src/routes.tsx` | Generated API client and route registry (M2+) |

Two items that will both touch these files aren't independently mergeable
even if their actual feature logic doesn't overlap - expect to merge one,
then either rebase the other on top or merge both into one integration
branch and resolve the conflict by hand once. Items that don't touch these
files (e.g. a self-contained package, or docs-only work) are safe to fully
parallelize.

The conformance table and the service container together mean **most M1
slices are not cleanly parallel**. Work that genuinely is: a new import
parser (ADR-0008), an FX provider adapter, docs-only changes, and anything
inside a single leaf package that registers nothing centrally.

When in doubt, sequential is always safe; parallel is an optimization when
the independence is clear.

## 4. Decide direct vs. spawned-agent

- **Do it directly** (no agent): single-file fixes, doc updates, small
  corrections, anything you can scope and verify in a few tool calls.
  Spawning an agent for this is overhead, not help.
- **Spawn an agent**: a bounded, self-contained feature slice. Use
  `isolation: "worktree"` so parallel agents don't collide on the working
  tree.
- **Fork vs. fresh agent**: fork when your own context is still reasonably
  sized - it's cheap via cache sharing and inherits all the reasoning
  already established this session, so the prompt can be a short
  directive. Prefer a fresh agent, briefed with the specific issue number
  plus which docs to read, once your own context is already large, or for
  a big standalone build.

Every agent prompt (fork or fresh) should include:

- Exact scope: what's in, what's already being handled elsewhere (name
  the other in-flight issues/branches so it doesn't duplicate work).
- Branch from local `main`, not `origin/main` - i.e. `git checkout -b
  <branch> main` after fetching, never `git checkout -b <branch>
  origin/main`. The latter looks equivalent but isn't: git auto-sets
  that branch's upstream tracking to `origin/main` itself, so a later
  `git push -u origin <branch>` silently resolves to pushing into
  `main` - exactly what the repo's push-to-main guards exist to catch,
  and did catch once already (a `feat/app-auth-usecases` agent branch
  hit this and needed `git branch --unset-upstream` before it could be
  pushed to its own name). Branching from local `main` doesn't trigger
  this, because autoSetupMerge only fires when the start-point is
  itself a remote-tracking ref.
- Run `make check` (fmt-check, vet, lint, test) and `make build` clean
  before finishing - the same two commands CI runs, and the only ones.
- Commit atomically, conventional commits, **no Claude co-author trailer**
  and no session link in PR bodies.
- Don't push or open a PR unless told to - the orchestrator handles
  integration once it's seen the result (this is what catches
  cross-agent conflicts before they hit GitHub).

## 5. Integrate - don't just trust the report

An agent's self-report is a claim, not a fact. Before pushing anything:

**Never merge a PR into `main` without the user's explicit, per-PR
consent.** CLAUDE.md is explicit here: "Request the user to do squash
merges... when merging into the main branch, or they may merge via
GitHub" - integrating stops at opening the PR and confirming it's green
and mergeable. A prior approval to merge one PR is not standing
permission for the next one, even within the same session; ask again (or
just hand over the link and say it's ready) each time. This applies
regardless of what the mechanics below describe - `gh pr merge` is
something to run only once the user has said, for that specific PR, to go
ahead.

- `cd` into the worktree and independently run the same
  build/vet/fmt/test commands.
- Before the first push of a new branch, check `git remote show origin`
  (or `git config branch.<name>.merge`) for that branch's tracking - it
  should be unset or already point at a same-named remote branch, never
  `refs/heads/main`. See step 4's branching note for why this can happen
  and how to fix it (`git branch --unset-upstream`) before pushing.
- Merge (or rebase on) current `origin/main` into the branch before
  calling a PR ready - do this even if the branch seemed fine when the
  agent finished, and do it again if the branch sits open for a while
  before merging. A branch built in parallel with another can be broken
  by whatever merged first without any git conflict at all. Re-run
  build/vet/fmt/test after the merge, not just before it.
- **When the branch's base (or a sibling PR you're stacking on) gets
  squash-merged, rebase onto it rather than merging.** A plain `git merge
  origin/main` drags the old branch's full pre-squash commit history in
  alongside the new squash commit - redundant, confusing history, even
  though the content ends up correct. Instead:
  `git rebase --onto origin/main origin/<old-base> <branch>` replays only
  the branch's own commits on top of the real squashed `main`. This is
  provably the intended shape, not just tidier: once the old base branch
  is deleted, GitHub itself auto-retargets the PR's `base` to `main`. When
  several sibling PRs share a now-merged base and get squash-merged one at
  a time, each remaining PR needs this rebase again after every one of its
  siblings lands - step 3's hotspot table means this is a near-certainty,
  not an edge case, for any batch of parallel PRs that touch the same
  hotspots.
- **A dependency change on the branch you rebased onto needs a real
  install, not just a clean rebase.** If `origin/main` (or whatever you
  rebased onto) added a package (`package.json`/`package-lock.json`,
  `go.mod`/`go.sum`), run `npm install` / `go mod download` in the
  worktree before re-running checks - otherwise `tsc`/`vitest`/`go build`
  fail on a module the lockfile now expects but `node_modules`/the module
  cache doesn't have yet.
- **A passing local check is not the same as a pushed check.** `make
  check` validates the working tree, not what's about to be pushed. A fix
  made after resolving a rebase conflict (a Prettier reformat, a stray
  edit) has to actually be committed - via `git commit --fixup <commit> &&
  git rebase -i --autosquash <base>` if it belongs folded into an earlier
  commit in the same rebase, otherwise a plain new commit - before
  pushing. Run `git status --short` immediately before every push and
  confirm it's clean; a CI failure on a commit that "already passed
  locally" almost always means the fix that made it pass was never
  committed.
- Check `gh api repos/<org>/<repo>/pulls/<N> --jq '{mergeable,
mergeable_state}'` once a PR exists, as a confirmation it's actually
  mergeable (`true`/`clean` once CI settles) before telling the user it's
  ready.
- Once the user has explicitly said to merge this PR: `gh pr merge
  --squash --delete-branch` can report a failure that's only the
  local-branch deletion (typically because a worktree still has that
  branch checked out), not a failed merge. Confirm the real outcome with
  `gh pr view <N> --json state,mergedAt` before treating the error as a
  problem to fix.
- Resolve conflicts by hand for anything additive/mechanical; stop and ask
  if a conflict looks like a real semantic disagreement rather than two
  independent additions landing in the same spot.
- Write the PR body in `pull_request_template.md`'s section structure -
  Summary, Closes (a literal "Closes #N" per issue this PR resolves -
  delete the section if it doesn't close anything - so GitHub auto-closes
  the issue on merge instead of a separate `gh issue close` call, which
  auto mode's permission classifier has been observed to block as a
  visible action), Changes, Dependencies, Artefacts (delete this section if
  there's nothing to attach), Testing. `gh pr create --body` bypasses
  GitHub's auto-populated template entirely, so this only happens if
  the body is written to match it deliberately. This doesn't mean
  dropping content that doesn't fit a literal section name - a scoping
  note on what the issue text got wrong, or what this PR unblocks, are
  worth keeping; fold them into Changes or Dependencies rather than
  inventing a new top-level heading for them.
- **Testing needs actual manual steps, not just automated-command
  output.** The template's own placeholder text asks for "manual steps
  to verify the change works" - a reviewer should be able to follow
  concrete steps to check the feature and likely regressions
  themselves, not just read that `make check` passed (that belongs
  there too, but as a supplement, not a replacement). When the change
  ships with a reachable surface (CLI/HTTP/web), give the literal
  commands or click-through steps against the running binary - this is
  also where the Web UI screenshot/recording workflow CLAUDE.md
  describes for UI surfaces produces the Artefacts section's content.
  When it doesn't yet - an app-layer-only slice like issue #55, whose
  surface wiring is a separate, later issue - say so plainly rather
  than implying a walkthrough that doesn't exist, and give the nearest
  real equivalent: specific `go test -run <name> -v` invocations mapped
  one-to-one to the PR's individual acceptance-criteria scenarios, not
  a single blanket `go test ./...`, so a reviewer can run one and see
  exactly what it proves.

## 6. When to create a new issue

Not just picking up existing issues - a session finds new work constantly.
Rule of thumb: if it's fixed in the same session, in the same PR (or a
small sibling PR), it doesn't need an issue - just do it and say so in the
report. If it's _not_ getting done right now, it needs an issue, or it's
just going to get lost. Concretely:

- **Mid-task discovery of something out of scope** for the current work -
  don't scope-creep the current PR to fix it and don't silently ignore it
  either. Open an issue.
- **A natural follow-up revealed by finishing something** - open an issue
  for it rather than letting the current PR grow to cover it.
- **A dependency's own "Out of scope" section points at a follow-up that
  was never actually created.** This has happened at least once for real:
  #133 (`RecordTransfer`) explicitly deferred "CLI/API request shape
  changes to actually send two different currencies on a transfer" to "a
  separate surface issue" - but when #136/#137 (the CLI/API surface
  issues) were scoped afterward, that thread wasn't picked up, and their
  scopes just said "drop the cross-currency rejection." The gap sat
  silent for three more issues until a web-UI slice (#138) tripped over
  it. When step 1 reads a candidate issue's body, also skim the "Out of
  scope" sections of the issues it depends on for language like "separate
  issue," "future change," or "not this issue" - then check
  `gh issue list --state all` for whether that separate issue actually
  exists. If it doesn't, that's exactly this pattern: open it now, before
  picking up the issue that assumed it was already tracked.
- **A deferred/future idea surfaces** that isn't in scope for the current
  milestone. Open it with the appropriate deferred/future label, not just
  a mention in conversation or a docs/ paragraph.
- **Planning a new milestone's components** - this one is not automatic.
  When step 1 flags a milestone transition, that's a prompt to surface to
  the user, not a trigger to start filing issues: propose a candidate
  scope for the next milestone and ask before creating anything. What
  ships next is a product decision, not a mechanical one. Once the user
  has confirmed scope, seed it the way the original components were: one
  issue per component, each citing the specific doc section or ADR that
  frames it, each stating explicit out-of-scope boundaries and
  dependencies on other issues.

  **Give the milestone a name, not just a number and a description.**
  Title format is `M<n> <Name> - <scope>`, e.g. `M1 Arda - Ledger core,
  CLI, and REST API`. Names come from **fantasy and sci-fi worlds and
  planets** — Arda, Arrakis, Discworld, Solaris, Hyperion, Earthsea,
  Trantor. Pick one that fits the milestone's character rather than
  working through a list in order: M1 is Arda, the world itself, which
  is what everything after it sits on. A name gives the milestone
  something to be referred to in conversation, commits, and PR titles
  that isn't "the current one", which stops meaning anything the moment
  it isn't.

  Name it when the milestone is created, not before — naming M3 through
  M8 up front is the same speculative work as scoping them up front, and
  their scope will move. Keep `docs/architecture.md` §8 and §9 in sync
  with whatever gets chosen, in the same PR.

  **The same scheme will apply to releases** once there's release
  tooling to apply it to — there's no release workflow, tag convention,
  or `release.yaml` yet, and none is needed until something is
  shippable. When that lands, a release takes the name of the milestone
  it completes rather than inventing a parallel vocabulary.

Before creating, `gh issue list --state all` to check it doesn't already
exist (open or closed) - don't duplicate.

## 7. Close the loop

As part of the PR that completes or changes scope of a milestone item
(not a follow-up, not end-of-session cleanup):

- Update the architecture/status doc's Status section, then check whether
  the change is user-visible or dev-visible - update whichever existing
  docs cover that ground as part of the same PR, not a later docs pass.
  If no existing doc is really the right home for something genuinely new,
  create one rather than stretching an existing doc to cover it, and link
  it in from wherever a reader would actually arrive at it.
- Reference the GitHub issue it resolves via a "Closes #N" line in the PR
  body (the template's Closes section) - GitHub closes it automatically on
  merge, so there's no separate `gh issue close` call needed for the
  fully-resolved case. If only partially addressed, comment on the issue
  explaining what's left instead of closing it.
- Grep the changed files for anything that looks like an internal
  reference (an ADR, a package name, an issue number) leaking into a
  user-facing string before calling it done.

## 8. Report back

End with a concise summary: what shipped (PR links), what's still open
and why (waiting on review, waiting on a dependency), and what the
natural next unblocked item is. Don't re-explain the architecture - the
user has read it; say what changed.
