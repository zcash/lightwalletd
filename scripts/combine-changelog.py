#!/usr/bin/env python3
"""Combine changelog.d/ fragments into a new CHANGELOG.md section.

Each pending entry lives in its own file under changelog.d/, named
<slug>.<category>.md, so PRs never touch CHANGELOG.md and never conflict there.
At release time this script folds the fragments into one dated section and
deletes them. See changelog.d/README.md for the fragment format.

Usage:
    scripts/combine-changelog.py X.Y.Z [YYYY-MM-DD]

The date defaults to today. The script edits CHANGELOG.md and removes the
fragment files, then prints a reminder to review the diff. It does not commit.
"""

import datetime
import os
import re
import subprocess
import sys

# Keep a Changelog's category order.
CATEGORIES = ["Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"]
CATEGORY_BY_KEY = {c.lower(): c for c in CATEGORIES}
FRAGMENT_RE = re.compile(r"^[a-z0-9][a-z0-9-]*\.(%s)\.md$" % "|".join(CATEGORY_BY_KEY))

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CHANGELOG = os.path.join(ROOT, "CHANGELOG.md")
FRAGMENT_DIR = os.path.join(ROOT, "changelog.d")


def die(msg):
    sys.stderr.write("combine-changelog: " + msg + "\n")
    sys.exit(1)


def parse_args(argv):
    if not (2 <= len(argv) <= 3):
        die("usage: combine-changelog.py X.Y.Z [YYYY-MM-DD]")
    version = argv[1].lstrip("v")
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        die("version %r must look like X.Y.Z" % argv[1])
    if len(argv) == 3:
        date = argv[2]
        if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", date):
            die("date %r must be YYYY-MM-DD" % date)
    else:
        date = datetime.date.today().isoformat()
    return version, date


def collect_fragments():
    """Return {category: [(filename, text), ...]} and the list of paths."""
    if not os.path.isdir(FRAGMENT_DIR):
        die("no changelog.d/ directory")
    groups = {}
    paths = []
    for name in sorted(os.listdir(FRAGMENT_DIR)):
        if not name.endswith(".md") or name == "README.md":
            continue
        m = FRAGMENT_RE.match(name)
        if not m:
            die("fragment %r must be named <slug>.<category>.md, where slug is "
                "lowercase letters, digits and hyphens, and category is one of: "
                "%s" % (name, ", ".join(c.lower() for c in CATEGORIES)))
        category = CATEGORY_BY_KEY[m.group(1)]
        path = os.path.join(FRAGMENT_DIR, name)
        with open(path, encoding="utf-8") as f:
            text = f.read().strip("\n")
        if not text.strip():
            die("fragment %r is empty" % name)
        groups.setdefault(category, []).append((name, text))
        paths.append(path)
    if not paths:
        die("no fragments found in changelog.d/")
    return groups, paths


def as_bullet(text):
    """Turn a plain-text fragment into a list item, keeping its line breaks.

    A fragment that already starts with "- " is used as written.
    """
    lines = [ln.rstrip() for ln in text.strip().splitlines()]
    if lines[0].startswith("- "):
        return "\n".join(lines)
    out = []
    for ln in lines:
        if not ln:
            if out and out[-1]:
                out.append("")
        elif not out:
            out.append("- " + ln.lstrip())
        else:
            out.append("  " + ln.lstrip())
    return "\n".join(out)


def build_section(version, date, groups):
    lines = ["## [%s] - %s" % (version, date), ""]
    for category in CATEGORIES:
        if category not in groups:
            continue
        lines.append("### " + category)
        lines.append("")
        for _, text in groups[category]:
            lines.append(as_bullet(text))
            lines.append("")
    return "\n".join(lines).rstrip("\n") + "\n"


def insertion_point(lines):
    """Index of the line to insert the new section before.

    After the intro, and after an [Unreleased] section if one is still present,
    so the newest release sits at the top of the version history.
    """
    headings = [i for i, ln in enumerate(lines) if ln.startswith("## [")]
    if not headings:
        die("CHANGELOG.md has no '## [' version headings")
    first = headings[0]
    if lines[first].startswith("## [Unreleased]"):
        return headings[1] if len(headings) > 1 else len(lines)
    return first


def main():
    version, date = parse_args(sys.argv)
    groups, paths = collect_fragments()
    section = build_section(version, date, groups)

    with open(CHANGELOG, encoding="utf-8") as f:
        content = f.read()
    lines = content.splitlines(keepends=True)
    at = insertion_point(lines)
    block = section if section.endswith("\n") else section + "\n"
    lines[at:at] = [block + "\n"]
    with open(CHANGELOG, "w", encoding="utf-8") as f:
        f.write("".join(lines))

    try:
        subprocess.run(["git", "rm", "-q"] + paths, cwd=ROOT, check=True)
        subprocess.run(["git", "add", CHANGELOG], cwd=ROOT, check=True)
    except (OSError, subprocess.CalledProcessError):
        for path in paths:
            os.remove(path)

    count = sum(len(v) for v in groups.values())
    print("Added [%s] - %s from %d fragment(s) across %d categor(y/ies)."
          % (version, date, count, len(groups)))
    print("Review the result with: git diff --cached CHANGELOG.md changelog.d")


if __name__ == "__main__":
    main()
