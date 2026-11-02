# Pending changelog entries

A PR that needs a changelog entry adds a file here instead of editing
`CHANGELOG.md`. Each PR touches its own file, so they never conflict on the
changelog, and the entries are combined into a dated section when a release is
cut. This is the same idea as
[towncrier](https://towncrier.readthedocs.io/) and Bitcoin Core's release
notes fragments.

## Adding an entry

Create one file per entry, named `<slug>.<category>.md`:

- `<slug>` is short and unique, made of lowercase letters, digits and
  hyphens. Usually it's the branch topic, e.g. `metrics-http-timeouts`. The PR
  number works too once you have one.
- `<category>` is one of the [Keep a Changelog](https://keepachangelog.com/)
  sections, lowercased: `added`, `changed`, `deprecated`, `removed`, `fixed`,
  `security`.

The file holds the entry as plain text, with no leading `- ` and no indent.
The release script makes it a list item, adding the `- ` and indenting the
lines after the first, and keeps your line breaks. Wrap lines at 78 columns,
so they fit within 80 once indented. A blank line starts a second paragraph
of the same entry. For a security fix, reference the advisory or fixing PR.

A PR can add more than one fragment, including several in the same category.
Give each its own slug, e.g. `sendtx-budget.security.md` and
`sendtx-flag.added.md`, or `sendtx-budget.security.md` and
`sendtx-copies.security.md`.

Within a category, entries appear in alphabetical order of filename. To put
them in a different order, prefix the slugs with numbers, e.g.
`01-sendtx-budget.security.md` and `02-sendtx-copies.security.md`.

Example, `changelog.d/metrics-http-timeouts.security.md`:

```
The Prometheus metrics HTTP server now sets read, write and idle timeouts, so
a client that opens a connection and sends a partial request can no longer pin
it open indefinitely (GHSA-w24w-x7vw-f2w8).
```

## Cutting a release

`scripts/combine-changelog.py X.Y.Z` folds every fragment into a new
`## [X.Y.Z] - DATE` section at the top of the version history, grouped by
category, and deletes the fragments. Review the result before committing. See
`RELEASING.md`.

## CI

CI runs `scripts/check-changelog.py` on every PR. It rejects a misnamed
fragment, and it rejects a PR that edits `CHANGELOG.md` directly, since entries
belong in fragments. A release PR passes because `combine-changelog.py` deletes
the fragments it folds in. For the rare hand edit, such as fixing a typo in an
already-released entry, add the `skip-changelog-check` label.
