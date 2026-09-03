# Releasing lightwalletd

Checklist for cutting a release. The steps that are easy to forget are called
out as **Gotcha**.

## 1. Choose the version

The CHANGELOG follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/)
and Rust's notion of [Semantic Versioning](https://semver.org/), where a `0.x`
release bumps the **minor** for breaking changes and the **patch** otherwise.

For example, v0.5.0 took a minor bump because it removed the (never used)
`CompactBlock.protoVersion` field and changed `GetMempoolStream` to report
`Height: 0`; v0.5.1 and v0.5.2 were patch releases carrying only fixes.

**Gotcha:** do not edit `common.Version`. There is a variable that looks like
the place to set it:

```go
// common/common.go — 'make build' will overwrite this string
var Version = "v0.0.0.0-dev"
```

but it is only the placeholder used by plain `go build`. The Makefile
overwrites it at link time from the git tag:

```make
VERSION := `git describe --tags`
LDFLAGSSTRING :=-X github.com/zcash/lightwalletd/common.Version=$(VERSION)
```

So **the tag is the version** — no source file changes for a release, and a
build from an untagged tree reports whatever `git describe` produces (which is
why an untagged clone can report a misleading version).

## 2. Update the CHANGELOG (in the release PR)

Rename the `## [Unreleased]` heading to `## [X.Y.Z] - YYYY-MM-DD`, and add a
fresh `## [Unreleased]` above it only if there is already unreleased work to
put there.

**Gotcha:** this is the step most often missed, and missing it compounds
silently. The heading was never renamed for v0.4.18, v0.4.19 or v0.5.0, so by
the time v0.5.0 shipped, `[Unreleased]` held three releases' worth of entries.
Untangling it meant splitting them across `[0.5.0]`, `[0.4.19]` and `[0.4.18]`
by checking which entries were present in the CHANGELOG at each release tag.
Naming the section while cutting the release is what prevents that.

The CHANGELOG is the **only** in-repo release artifact. Everything below
happens after the merge.

## 3. Merge and tag

Merge the release PR, then tag the merge commit on `master`:

```sh
git tag v0.5.2 <merge-commit>
git push upstream v0.5.2
```

**Gotcha:** the tag is `vX.Y.Z`, *with* the `v`. (Advisory version fields want
it *without* — see step 5.)

Pushing a `v*.*.*` tag triggers CI's `build` job only. It does **not** publish
anything.

## 4. Create the GitHub release

Create a release from the tag, using the CHANGELOG section as the notes.

**Gotcha:** this is the step that actually ships. `.github/workflows/CI.yaml`
runs `docker_push` on `if: github.event_name == 'release'`, which builds and
pushes to public Docker Hub:

- `electriccoinco/lightwalletd:<tag>` and `electriccoinco/lightwalletd:latest`
- mirrored to `zodlinc/lightwalletd:<tag>` and `:latest`

Two consequences:

- Publishing the release overwrites `:latest`, which production deployments
  pull. Do it deliberately, not as bookkeeping after the fact.
- **Never publish a GitHub release from a fork or a private security repo.**
  The same workflow runs there, so it would push images — and, for an
  embargoed fix, disclose it early. Keep such releases as *drafts*: a draft
  emits no `release: published` event and creates no tag.

## 5. Security releases: publish the advisories

If the release contains fixes for GitHub security advisories, publish them once
the release is out. Until then the fix is public with no advisory telling
operators to upgrade.

For each advisory, set the affected package (ecosystem `Go`, package
`github.com/zcash/lightwalletd`) and:

- **Affected versions** — e.g. `<= 0.5.1`
- **Patched versions** — e.g. `0.5.2`

**Gotcha:** state the range as of the *fix*, not as of the report. Advisories
are often filed months earlier, so a report may say `<= 0.4.19` while the bug
in fact persisted through later releases. Publishing the stale range tells
users of intermediate versions they are unaffected when they are not.

**Gotcha:** version fields here take the bare version, `0.5.2`, **not** `v0.5.2`.

**Gotcha:** in the advisory editor, fill the affected-product fields and click
**Update security advisory** *before* publishing. Publishing does not commit
unsaved edits, and the fields silently stay empty — leaving an advisory that
names no fixed version at all.

Advisories that will not be published (duplicates, Not-a-Vulnerability) should
not be referenced by GHSA ID in the CHANGELOG, since the ID never resolves
publicly. Cite the fixing PR instead.

## Quick checklist

- [ ] Version chosen (minor bump if anything is breaking)
- [ ] CHANGELOG `[Unreleased]` renamed to `[X.Y.Z] - YYYY-MM-DD`
- [ ] Release PR merged
- [ ] Tag `vX.Y.Z` pushed
- [ ] GitHub release created (this pushes Docker images and moves `:latest`)
- [ ] Docker images confirmed on Docker Hub
- [ ] Security advisories published, with affected/patched ranges correct
