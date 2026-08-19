#!/usr/bin/env python3
"""Regenerate PACKAGE_INDEX.md's ## Summary and ## Files sections.

These two sections are pure file-tree data (per-directory counts and a full
sorted file listing) and previously had to be hand-typed after every task,
which is exactly the kind of thing that silently goes stale — the listing
still named a file deleted three tasks ago, and every count was off by
however many files the author forgot to recount by hand.

Everything else in PACKAGE_INDEX.md (the title/date/inventory-numbers
preamble and every "## Task N additions" narrative section) is curated prose
that summarizes *why* a change happened, which this script cannot derive
from the file tree and does not touch: edit that part of the file directly,
the same way it has always been maintained.
"""

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "PACKAGE_INDEX.md"

SUMMARY_HEADING = "## Summary"
FILES_HEADING = "## Files"

# Mirrors .gitignore: build/dependency/cache trees and generated evidence are
# not part of the package. .git and .claude are not in .gitignore (git never
# needs to ignore itself, and .claude is local session tooling) so they are
# listed explicitly.
SKIP_DIR_NAMES = {".git", ".claude", "node_modules", "dist", "__pycache__", "reports", ".security-tools"}
SKIP_PATH_PREFIXES = ("qualification/evidence/",)
SKIP_FILE_SUFFIXES = (".pyc", ".pyo", ".pyd", ".test")
SKIP_FILE_NAMES = {"coverage.out"}

# Named subsets reported in ## Summary, alongside the repo-wide total.
CATEGORY_DIRS = {
    "docs": "docs",
    "adrs": "adr",
    "tasks": "tasks/issues",
    "milestones": "tasks/milestones",
    "contracts": "contracts",
    "prompts": "prompts",
    "templates": "templates",
}


def is_secret_env_file(name: str) -> bool:
    return name == ".env" or (name.startswith(".env.") and name != ".env.example")


def should_skip(relative_parts: tuple[str, ...]) -> bool:
    if any(part in SKIP_DIR_NAMES for part in relative_parts[:-1]):
        return True
    relative_posix = "/".join(relative_parts)
    if relative_posix.startswith(SKIP_PATH_PREFIXES):
        return True
    name = relative_parts[-1]
    if is_secret_env_file(name):
        return True
    if name in SKIP_FILE_NAMES:
        return True
    if any(name.endswith(suffix) for suffix in SKIP_FILE_SUFFIXES):
        return True
    return False


def iter_package_files():
    for path in ROOT.rglob("*"):
        if not path.is_file():
            continue
        relative = path.relative_to(ROOT)
        if should_skip(relative.parts):
            continue
        yield relative.as_posix()


def count_dir(relative_dir: str) -> int:
    base = ROOT / relative_dir
    if not base.is_dir():
        raise SystemExit(f"expected directory missing: {relative_dir}")
    count = 0
    for path in base.rglob("*"):
        if path.is_file() and not should_skip(path.relative_to(ROOT).parts):
            count += 1
    return count


def count_codex_skills() -> int:
    base = ROOT / ".codex/skills"
    return sum(1 for entry in base.iterdir() if entry.is_dir())


def render_summary(all_files: list[str]) -> str:
    counts = {key: count_dir(path) for key, path in CATEGORY_DIRS.items()}
    counts["codex_skills"] = count_codex_skills()
    order = ["docs", "adrs", "tasks", "milestones", "codex_skills", "contracts", "prompts", "templates"]
    lines = [SUMMARY_HEADING, ""]
    lines += [f"- {key}: {counts[key]}" for key in order]
    lines.append(f"- total source files (excluding local secrets/build/dependency/cache trees): {len(all_files)}")
    lines += ["", ""]
    return "\n".join(lines)


def render_files(all_files: list[str]) -> str:
    lines = [FILES_HEADING, ""]
    lines += [f"- `{path}`" for path in sorted(all_files)]
    return "\n".join(lines) + "\n"


def split_existing(text: str) -> tuple[str, str]:
    """Return (head, narrative): head runs through the line before
    ## Summary; narrative runs from the first '## ' heading after ## Summary
    through the line before ## Files. Both are preserved verbatim."""
    lines = text.split("\n")
    try:
        summary_index = lines.index(SUMMARY_HEADING)
    except ValueError:
        raise SystemExit(f"{OUTPUT}: no '{SUMMARY_HEADING}' heading found")
    narrative_start = None
    for index in range(summary_index + 1, len(lines)):
        if lines[index].startswith("## ") and lines[index] != SUMMARY_HEADING:
            narrative_start = index
            break
    if narrative_start is None:
        raise SystemExit(f"{OUTPUT}: no narrative section found after '{SUMMARY_HEADING}'")
    try:
        files_index = lines.index(FILES_HEADING, narrative_start)
    except ValueError:
        raise SystemExit(f"{OUTPUT}: no '{FILES_HEADING}' heading found after the narrative section")
    head = "\n".join(lines[:summary_index])
    narrative = "\n".join(lines[narrative_start:files_index])
    return head, narrative


def render(existing_text: str) -> str:
    head, narrative = split_existing(existing_text)
    all_files = list(iter_package_files())
    return "\n".join([head, render_summary(all_files), narrative, render_files(all_files)])


def main() -> None:
    if not OUTPUT.is_file():
        raise SystemExit(f"{OUTPUT} does not exist; this script only regenerates its data sections")
    existing = OUTPUT.read_text(encoding="utf-8")
    rendered = render(existing)
    if "--check" in sys.argv[1:]:
        if rendered != existing:
            raise SystemExit("PACKAGE_INDEX.md ## Summary/## Files are stale; run scripts/generate-package-index.py")
        print("PACKAGE_INDEX.md data sections: PASS")
        return
    OUTPUT.write_text(rendered, encoding="utf-8")
    print(f"Regenerated {OUTPUT.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
