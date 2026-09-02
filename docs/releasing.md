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
  previous tag, grouped into Features / Fixes / Documentation / Other —
  this is the **same mechanism** used to update the checked-in
  [`CHANGELOG.md`](../CHANGELOG.md) below, so the two are never
  independently-maintained texts.
- Publishes a GitHub Release with the built archives, checksums, and that
  changelog as the release body.

`.github/workflows/release.yaml` runs this on every push of a tag matching
`v*.*.*`. Pushing a tag is the only trigger — there is no manual dispatch
and no bot watching `main`.

## Semver policy

Conventional commits are the input; this is how they map to a version
bump:

| Commit shape | Pre-1.0 (`0.x.y`) | Post-1.0 (`x.y.z`) |
| --- | --- | --- |
| `fix:` | patch | patch |
| `feat:` | minor | minor |
| `feat!:` / `BREAKING CHANGE:` footer | minor (semver allows this pre-1.0) | major |
| anything else (`docs:`, `chore:`, `test:`, `refactor:`, …) | no release on its own | no release on its own |

**A milestone completing is not automatically a version bump rule** — it's
a judgment call made at release time, informed by what actually shipped
since the last tag. In practice: **M1 (Arda) ships as `v1.0.0`** — the
first release, and the point at which bodger is genuinely usable
end-to-end (domain model, CLI, REST API, derived balances), which is what
"1.0" should mean regardless of the pre-1.0 table above. Later milestones
each get their own release when they're done; whether that's a minor or
major bump depends on whether anything in it breaks a documented contract
(the REST API's request/response shapes, the CLI's flags and output, the
export format) — check the actual diff, don't assume from the milestone
number.

A milestone can also span more than one release, or a release can land
mid-milestone for something worth shipping early — the tag doesn't have to
line up with a milestone boundary. It only carries the milestone's name in
its title (below) when it *is* the release that completes one.

## Release title and the milestone name

A bare semver tag (`v1.0.0`) doesn't carry the milestone name
(`M1 Arda`) — GoReleaser can't infer that from the tag string alone, and
[`architecture.md` §8](architecture.md#8-milestones) is explicit that a
release should take its milestone's name rather than inventing a second
vocabulary. So the name travels with the **tag object itself**: create an
**annotated** tag whose message is the milestone name, and
`release.yaml` reads it back to build the title
(`v1.0.0 — M1 Arda`) before invoking GoReleaser. A tag with no message (or
a release that doesn't complete a milestone) falls back to the bare tag
name as the title — see the exact commands below.

This was a deliberate choice over passing the name as a manual workflow
input: encoding it in the tag means the same annotated tag, run locally
with `goreleaser release --skip=publish`, produces an identical title to
what CI will publish — one source of truth, no separate step to remember.

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
goreleaser release --skip=validate --skip=publish --clean
cat dist/CHANGELOG.md   # this is your new release's entry
git tag -d v0.0.0-preview
rm -rf dist
```

(`--skip=validate` bypasses GoReleaser's clean-working-tree/CI-environment
checks, which don't apply to a local preview; nothing is published.)

### 2. Open the routine PR

On a feature branch, prepend `dist/CHANGELOG.md`'s content — with a
version heading added on top — to the top of the repo's
[`CHANGELOG.md`](../CHANGELOG.md):

```markdown
## v1.0.0 — 2026-09-15

### Features
...
```

Same PR: confirm [`architecture.md` §9 Status](architecture.md#9-status)
already shows the milestone this release completes (if any) as done — it
should already be, per the "update Status in the same PR as the work"
convention, so this is usually a confirmation, not new content. Finish it
here if it was somehow missed.

Open the PR, get it reviewed, squash-merge it per the usual convention.

### 3. Tag the merge commit

Once merged, tag the resulting commit on `main` — **not** the pre-merge
branch tip, since squash-merge gives it a new SHA. `release.yaml` checks
this itself and refuses to run against a commit `main` hasn't reached, but
that check only saves you from the mistake after the fact — tag the merge
commit, not a branch tip, in the first place.

```sh
git checkout main && git pull
git tag -a v1.0.0 -m "M1 Arda"   # omit -m, or use a plain description, for a non-milestone release
git push origin v1.0.0
```

Use `git tag -s` instead of `-a` if you want the tag GPG-signed (a signing
key is already configured for this repository; it isn't required for
every commit, but a release tag is a reasonable place to use it — pass
`-s` in place of `-a` above, same `-m`).

Pushing the tag triggers `release.yaml`: it builds, generates the same
changelog as step 1 (now for real, from the real tag), and publishes the
GitHub Release with the title read from the tag message.

### 4. Verify

Check the [Releases page](https://github.com/anirudhgray/bodger/releases)
for the new release, its title, its changelog, and that all four archives
attached.

## Local dry runs

`goreleaser check` validates `.goreleaser.yaml` without building anything.
`goreleaser release --snapshot --clean --skip=publish` builds every
target locally without needing any tag at all (useful for checking the
build side after touching `.goreleaser.yaml`, ldflags, or
`internal/platform/version` — it won't generate a changelog, since
snapshot mode has no tag to diff against; use the throwaway-tag method
above for that).
