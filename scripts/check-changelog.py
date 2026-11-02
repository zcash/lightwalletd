#!/usr/bin/env python3
"""Check the changelog-fragment convention, for CI and local use.

Two checks:

1. Format (always): every file in changelog.d/ is named <slug>.<category>.md
   with a known category, so a typo like `.fixd.md` fails early.

2. Direct edit (only with --base/--head): a PR that changes CHANGELOG.md but
   deletes no changelog.d/ fragment is editing the changelog by hand, which the
   fragment convention replaces. A release PR runs combine-changelog.py, which
   both edits CHANGELOG.md and deletes the fragments, so it passes. The label
   escape hatch (ALLOW env or --allow) covers the rare hand edit, e.g. fixing a
   typo in an already-released entry.

Usage:
    scripts/check-changelog.py                       # format only
    scripts/check-changelog.py --base SHA --head SHA # + direct-edit guard
"""

import argparse
import os
import re
import subprocess
import sys

CATEGORIES = ("added", "changed", "deprecated", "removed", "fixed", "security")
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FRAGMENT_DIR = os.path.join(ROOT, "changelog.d")
FRAGMENT_RE = re.compile(r"^.+\.(%s)\.md$" % "|".join(CATEGORIES))
SKIP_LABEL = "skip-changelog-check"


def fail(msg):
    sys.stderr.write("check-changelog: " + msg + "\n")
    sys.exit(1)


def check_format():
    if not os.path.isdir(FRAGMENT_DIR):
        return
    bad = []
    for name in sorted(os.listdir(FRAGMENT_DIR)):
        if not name.endswith(".md") or name == "README.md":
            continue
        if not FRAGMENT_RE.match(name):
            bad.append(name)
    if bad:
        fail("fragment(s) misnamed: %s\n"
             "  expected <slug>.<category>.md, category one of: %s"
             % (", ".join(bad), ", ".join(CATEGORIES)))


def changed_files(base, head):
    mb = subprocess.run(["git", "merge-base", base, head], cwd=ROOT,
                        capture_output=True, text=True, check=True).stdout.strip()
    out = subprocess.run(["git", "diff", "--name-status", "--no-renames", mb, head],
                         cwd=ROOT, capture_output=True, text=True, check=True).stdout
    changes = []
    for line in out.splitlines():
        parts = line.split("\t")
        if len(parts) >= 2:
            changes.append((parts[0], parts[1]))
    return changes


def check_direct_edit(base, head, allow):
    changes = changed_files(base, head)
    changelog_edited = any(
        path == "CHANGELOG.md" and status[0] in "AM" for status, path in changes)
    fragment_deleted = any(
        status[0] == "D" and path.startswith("changelog.d/")
        and path.endswith(".md") and not path.endswith("/README.md")
        for status, path in changes)
    if changelog_edited and not fragment_deleted and not allow:
        fail("CHANGELOG.md was edited directly.\n"
             "  Add a changelog.d/<slug>.<category>.md fragment instead (see\n"
             "  changelog.d/README.md). If this is a release PR, run\n"
             "  scripts/combine-changelog.py, which deletes the fragments it\n"
             "  folds in. To override, add the '%s' label." % SKIP_LABEL)


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--base")
    p.add_argument("--head")
    p.add_argument("--allow", action="store_true",
                   help="permit a direct CHANGELOG.md edit")
    args = p.parse_args()

    check_format()

    allow = args.allow or os.environ.get("ALLOW_CHANGELOG_EDIT", "") == "true"
    if args.base and args.head:
        check_direct_edit(args.base, args.head, allow)
    elif args.base or args.head:
        fail("--base and --head must be given together")

    print("check-changelog: ok")


if __name__ == "__main__":
    main()
