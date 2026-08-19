#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "${script_dir}/.." && pwd)"
cd "${repo_root}"

export GOTOOLCHAIN=local
export GOWORK=off

go -C tools/contractcheck mod tidy -diff
go -C tools/contractcheck test -mod=readonly ./...
go -C tools/contractcheck vet -mod=readonly ./...
go -C tools/contractcheck run -mod=readonly ./cmd/contractcheck --root ../..
go -C tools/contractcheck mod verify

python3 - <<'PY'
from pathlib import Path
import re

root = Path.cwd()
numbers = {}
for path in (root / "tasks" / "issues").glob("*.md"):
    match = re.match(r"(\d+)-", path.name)
    if match:
        number = int(match.group(1))
        numbers.setdefault(number, []).append(path.name)

duplicates = {number: names for number, names in numbers.items() if len(names) > 1}
if duplicates:
    raise SystemExit(f"duplicate task numbers: {duplicates}")
if not numbers:
    raise SystemExit("no numbered tasks found")
missing = [number for number in range(1, max(numbers) + 1) if number not in numbers]
if missing:
    raise SystemExit(f"missing task numbers: {missing}")

skills = list((root / ".codex" / "skills").glob("*/SKILL.md"))
if not skills:
    raise SystemExit("no Codex skills found")
for path in skills:
    if not path.read_text(encoding="utf-8").strip().startswith("#"):
        raise SystemExit(f"invalid skill: {path}")

print(f"Task numbering contiguous: 001-{max(numbers):03d}")
print("Codex skill layout valid")
PY
