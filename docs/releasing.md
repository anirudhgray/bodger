# Releasing

This is the runbook for cutting a bodger release: what bumps the version,
how the changelog gets written, and the exact commands that turn a merged
PR into a tagged, published release. It assumes the reader already knows
[`contributing.md`](contributing.md)'s branch/PR/squash-merge conventions —
this doc is the layer on top, for the specific act of releasing.

## How releases are built

[GoReleaser](https://goreleaser.com/) does the mechanical work, configured
in [`.goreleaser.yaml`](../.goreleaser.yaml):

- Builds the `bodger` binary for `linux`/`darwin` × `amd64`/`arm64`,
  `CGO_ENABLED=0`, matching the `make build` invocation.
- Injects the tag, commit, and build date into
  `internal/platform/version` via `-ldflags -X`, so `bodger --version`
  reports real build metadata on a released binary (`dev` on anything
  built locally with plain `go build` or `make build`).
- Generates a changelog from conventional-commit history since the
  previous tag — this is the **same mechanism** used to update the
  checked-in [`CHANGELOG.md`](../CHANGELOG.md) below, so the two are never
  independently-maintained texts.
- Publishes a GitHub Release with the built archives, checksums, and that
  changelog as the release body — with a footer linking back to
  [`CHANGELOG.md`](../CHANGELOG.md), since a single release's notes are
  only ever one slice of the full history.

`.github/workflows/release.yaml` runs this on every push of a tag matching
`v*.*.*`. Pushing a tag is the only trigger — there is no manual dispatch
and no bot watching `main`.

## Changelog format

[`CHANGELOG.md`](../CHANGELOG.md) follows [Keep a
Changelog](https://keepachangelog.com/en/1.1.0/): an `## [Unreleased]`
section always sits at the top, and each release gets its own
`## [X.Y.Z] - YYYY-MM-DD` section below it, with entries grouped under
Keep a Changelog's own categories (Added, Changed, Deprecated, Removed,
Fixed, Security) — not conventional commits' type names.
`.goreleaser.yaml`'s `changelog.groups` maps `feat` → Added, `fix` →
Fixed, `refactor`/`perf` → Changed; internal-only commit types (`docs`,
`chore`, `test`, `ci`, `style`, `build`) are excluded entirely rather than
dumped into a catch-all, since Keep a Changelog is for changes a user of
the released binary would actually notice. Each entry credits its author
— GitHub handle when GoReleaser could resolve one (only possible via the
GitHub API, which needs a previous tag to diff against — never true for
the first release, and not guaranteed thereafter), otherwise the git
author name, which is always available.

Each entry's short SHA links to its commit (`changelog.format` in
[`.goreleaser.yaml`](../.goreleaser.yaml)). The `(#N)` PR reference
alongside it — GitHub's own squash-merge suffix, riding along inside the
commit message text — is **not** linked: GoReleaser's changelog format
template has no Sprig funcmap available (confirmed by hitting `function
"regexReplaceAll" not defined` trying it), so there's no way to turn text
embedded in `.Message` into a link from this field.

Each version heading is a clickable link — `## [X.Y.Z] - YYYY-MM-DD`
resolves via a same-named link reference at the bottom of the file
(`[X.Y.Z]: https://github.com/anirudhgray/bodger/compare/vPREV...vX.Y.Z`,
or straight to the release tag for the very first version, which has no
predecessor to diff against). **Add one when you add the version
section** — see step 2 below — the same way `[Unreleased]`'s own link
target moves to point at the new latest tag.

**Deprecated and Removed have no commit-type mapping** — a `fix` or a
`feat` can just as easily do either — so GoReleaser never populates them.
Add either by hand, editing the generated content before it goes into the
routine PR, when a release actually deprecates or removes something.

One deliberate simplification versus purist Keep a Changelog practice:
entries are generated in bulk at release-cut time from commit history,
not hand-written incrementally in each contributing PR. `[Unreleased]`
therefore stays empty between releases rather than accumulating entries
as they land — it exists as the placeholder a release fills in and
replaces, per the procedure below.

## Semver policy

Conventional commits are the input; this is how they map to a version
bump:

| Commit shape | Pre-1.0 (`0.x.y`) | Post-1.0 (`x.y.z`) |
| --- | --- | --- |
| `fix:` | patch | patch |
| `feat:` | minor | minor |
| `feat!:` / `BREAKING CHANGE:` footer | minor (semver allows this pre-1.0) | major |
| anything else (`docs:`, `chore:`, `test:`, `refactor:`, …) | no release on its own | no release on its own |

**A milestone completing is not automatically a version bump rule, and
isn't automatically the 0.x → 1.0.0 transition either** — both are
judgment calls made at release time, not read off a milestone number.
bodger starts, and stays, pre-1.0 (`0.x.y`) until there's an actual
commitment to hold something stable — the REST API's request/response
shapes, the CLI's flags and output, the export format — across releases.
Crossing to `1.0.0` is a deliberate decision to make that commitment,
made when it's true, not a label attached to whichever milestone happens
to finish first. Each milestone still gets its own release when it's
done; whether that's a minor or major (pre- or post-1.0) bump depends on
whether anything in it breaks a documented contract — check the actual
diff, don't assume from the milestone number.

A milestone can also span more than one release, or a release can land
mid-milestone for something worth shipping early — the tag doesn't have to
line up with a milestone boundary. It only carries the milestone's name in
its title (below) when it *is* the release that completes one.

## Release title and the milestone name

A bare semver tag (e.g. `v0.4.0`) doesn't carry a milestone's name (e.g.
`M<n> <Name>`) — GoReleaser can't infer that from the tag string alone,
and [`architecture.md` §8](architecture.md#8-milestones) is explicit that
a release should take its milestone's name rather than inventing a second
vocabulary. So the name travels with the **tag object itself**: create an
**annotated** tag whose message is the milestone name, and `release.yaml`
reads it back to build the title (`vX.Y.Z — M<n> <Name>`) before invoking
GoReleaser. A tag with no message (or a release that doesn't complete a
milestone) falls back to the bare tag name as the title — see the exact
commands below.

This was a deliberate choice over passing the name as a manual workflow
input: encoding it in the tag means the same annotated tag, run locally
with `goreleaser release --skip=publish`, produces an identical title to
what CI will publish — one source of truth, no separate step to remember.

## Adding freeform notes to a release

Everything above the generated changelog on a release page can carry
handwritten highlights too — the same tag annotation, extended. An
annotated tag's message is subject + blank line + body, exactly like a
commit: the subject is the milestone name (above); a second paragraph is
freeform Markdown, prepended above the changelog as the release body's
header (`release.header` in [`.goreleaser.yaml`](../.goreleaser.yaml),
populated from `RELEASE_NOTES_HEADER`, which `release.yaml` exports the
same way it exports `RELEASE_TITLE`).

Both the title and the notes body are **entirely optional** — a plain
`git tag vX.Y.Z && git push origin vX.Y.Z` with no annotation at all still
works exactly as it would without either feature: the title falls back to
the bare tag, and an empty notes body renders as nothing rather than an
error.

To add notes, pass a second `-m` (or write a full multi-line message with
no `-m`, which opens `$EDITOR`):

```sh
git tag -a vX.Y.Z -m "M<n> <Name>" -m "Whatever's worth calling out by hand — this is genuinely optional."
```

## Cutting a release: the routine PR, then the tag

Releases are cut via a small, ordinary PR — not a bot, not a manual tag
push with no PR trail. The sequence:

### 1. Preview the changelog

There's a chicken-and-egg problem: GoReleaser computes the changelog by
comparing the current tag to the previous one, but the tag doesn't exist
yet at the point you want to write the changelog entry. Resolve it with a
throwaway local tag:

```sh
git checkout main && git pull
git tag -a v0.0.0-preview -m "preview"
git push origin v0.0.0-preview
goreleaser release --skip=validate --skip=publish --clean
cat dist/CHANGELOG.md   # this is your new release's entry
git tag -d v0.0.0-preview
git push origin :refs/tags/v0.0.0-preview
rm -rf dist
```

(`--skip=validate` bypasses GoReleaser's clean-working-tree/CI-environment
checks, which don't apply to a local preview; nothing is published.)

**The throwaway tag must actually be pushed**, briefly — a purely local
tag isn't enough. `.goreleaser.yaml`'s `changelog.use: github` resolves
each entry's author credit via GitHub's REST compare API
(`.../compare/vPREV...v0.0.0-preview`), which 404s on a ref that only
exists locally. Push it, run the preview, then delete it both locally
and on the remote (`git push origin :refs/tags/v0.0.0-preview`) — same
throwaway spirit as before, just briefly visible on GitHub in the
meantime. If you'd rather not push even a throwaway tag, hand-build the
entry directly from `git log vPREV..main` instead: `sort: asc` in this
config sorts alphabetically by commit message text (not chronologically)
within each group, and no author suffix appears for this single-maintainer
repo despite the template's `{{ AuthorUsername }}`/`{{ AuthorName }}`
fallback — see any existing `CHANGELOG.md` entry for the exact format to
match by hand.

### 2. Open the routine PR

On a feature branch, edit the repo's [`CHANGELOG.md`](../CHANGELOG.md):

- Replace the empty `## [Unreleased]` section's body with
  `dist/CHANGELOG.md`'s content, and rename that heading to
  `## [X.Y.Z] - YYYY-MM-DD`.
- Add a fresh, empty `## [Unreleased]` section above it, ready for the
  next release.
- At the bottom of the file, add `[X.Y.Z]: .../compare/vPREV...vX.Y.Z`
  (the previous release's tag to this one), and repoint `[Unreleased]`'s
  own link at `.../compare/vX.Y.Z...HEAD` — see the exact form already in
  the file from the last release.

```markdown
## [Unreleased]

## [X.Y.Z] - 2026-09-15

### Added
...

### Fixed
...

[Unreleased]: https://github.com/anirudhgray/bodger/compare/vX.Y.Z...HEAD
[X.Y.Z]: https://github.com/anirudhgray/bodger/compare/vPREV...vX.Y.Z
```

Same PR: confirm [`architecture.md` §9 Status](architecture.md#9-status)
already shows the milestone this release completes (if any) as done — it
should already be, per the "update Status in the same PR as the work"
convention, so this is usually a confirmation, not new content. Finish it
here if it was somehow missed.

Open the PR, get it reviewed, squash-merge it per the usual convention.

If this release completes a milestone, also close its **native GitHub
Milestone** (separate from any issue — milestones don't close themselves
just because their last issue did):

```sh
gh api repos/anirudhgray/bodger/milestones/<number> -X PATCH -f state=closed
```

`gh` has no dedicated `milestone` subcommand for this — `gh api` against
the milestone's number (`gh api repos/anirudhgray/bodger/milestones --jq
'.[] | {number,title,open_issues}'` finds it) is the only way. Do this
once the PR above has merged, not before — the milestone's issues being
done is necessary but the release actually shipping is what the closed
state should reflect.

### 3. Tag the merge commit

Once merged, tag the resulting commit on `main` — **not** the pre-merge
branch tip, since squash-merge gives it a new SHA. `release.yaml` checks
this itself and refuses to run against a commit `main` hasn't reached, but
that check only saves you from the mistake after the fact — tag the merge
commit, not a branch tip, in the first place.

```sh
git checkout main && git pull
git tag -s vX.Y.Z -m "M<n> <Name>"   # omit -m, or use a plain description, for a non-milestone release
git push origin vX.Y.Z
```

Add a second `-m` here for freeform release notes — see [above](#adding-freeform-notes-to-a-release).

Release tags for this repository are **always GPG-signed** (`-s`, not
`-a` — a signing key is already configured). This isn't optional the way
it might be for an ordinary commit; don't substitute an unsigned tag as a
workaround for anything below.

**An agent-driven or otherwise non-interactive session must not run this
step itself — hand these two commands to a human to run from their own
interactive terminal.** `-s` needs a terminal `gpg-agent`/`pinentry` can
actually prompt through; with no real TTY to attach to, `git tag -s`
hangs waiting on a prompt that can never be answered, times out, and can
corrupt the terminal session outright — hit exactly this cutting v0.2.0
from a Claude Code session, which then (wrongly) pushed an unsigned tag
and a full public release off it as a workaround, both of which had to be
deleted and redone. There's no non-interactive workaround for the hang
(this is `gpg-agent`/`pinentry` behavior, not something `.goreleaser.yaml`
or this repo's tooling controls) and no unsigned substitute — the fix is
handing the two commands off, not finding a way to run them anyway.

Pushing the tag triggers `release.yaml`: it builds, generates the same
changelog as step 1 (now for real, from the real tag), and publishes the
GitHub Release with the title read from the tag message.

### 4. Verify

Check the [Releases page](https://github.com/anirudhgray/bodger/releases)
for the new release, its title, its changelog, and that all four archives
attached. If a milestone was closed above, confirm it shows closed on the
[Milestones page](https://github.com/anirudhgray/bodger/milestones) too.

## Local dry runs

`goreleaser check` validates `.goreleaser.yaml` without building anything.
`goreleaser release --snapshot --clean --skip=publish` builds every
target locally without needing any tag at all (useful for checking the
build side after touching `.goreleaser.yaml`, ldflags, or
`internal/platform/version` — it won't generate a changelog, since
snapshot mode has no tag to diff against; use the throwaway-tag method
above for that).
