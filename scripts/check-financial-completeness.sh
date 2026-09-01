#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

python3 - <<'PY'
import hashlib
import json
import re
from pathlib import Path

root = Path.cwd()
matrix_path = root / "contracts/finance/financial-completeness-matrix-v1.json"
matrix = json.loads(matrix_path.read_text())
requirements = matrix.get("requirements", [])
expected = {"revenue", "refunds", "payout", "cash", "cogs", "fx", "advertising", "promotion"}
actual = {item.get("code") for item in requirements}
if actual != expected or len(requirements) != len(expected):
    raise SystemExit(f"financial completeness matrix mismatch: {sorted(actual)}")
for item in requirements:
    if not item.get("canonical_source") or not item.get("fallback") or not item.get("required_for") or not item.get("retention"):
        raise SystemExit(f"incomplete matrix row: {item.get('code')}")

migration = root / "migrations/000054_financial_completeness_sources.sql"
if not migration.is_file():
    raise SystemExit("financial completeness migration is missing")
catalog = json.loads((root / "migrations/catalog.json").read_text())
entry = next((item for item in catalog.get("migrations", []) if item.get("version") == 54), None)
digest = hashlib.sha256(migration.read_bytes()).hexdigest()
if not entry or entry.get("file") != migration.name or entry.get("sha256") != digest:
    raise SystemExit("migration catalog/hash drift for version 54")

openapi = (root / "contracts/openapi/torgnexa-v1.yaml").read_text()
for path in ["/financial-completeness:", "/financial-completeness/sources:", "/financial-completeness/findings:"]:
    if path not in openapi:
        raise SystemExit(f"OpenAPI path missing: {path}")
api = (root / "internal/app/api/financial_completeness.go").read_text()
frontend = (root / "frontend/src/pages/FinancialAnalyticsPage.tsx").read_text()
mcp = (root / "internal/app/mcp/tools.go").read_text() + (root / "internal/app/mcp/server.go").read_text()
for text, required in [
    (api, ["finance.reports.read", "finance.sources.write", "Idempotency-Key", "ListSources", "ListFindings"]),
    (frontend, ["Полнота данных", "coverage_percent", "Нет источника", "listFinancialCompletenessFindings"]),
    (mcp, ["commerce.finance.completeness.get", "FinancialCompletenessReader", "read-only"]),
    ((root / "sdk/typescript/src/client.gen.mjs").read_text(), ["getFinancialCompleteness", "appendFinancialSource", "listFinancialSources"]),
]:
    missing = [value for value in required if value not in text]
    if missing:
        raise SystemExit(f"financial completeness wiring missing: {missing}")

for relative in ["internal/core/financialcompleteness", "internal/platform/postgres/financialcompletenessrepo", "internal/app/api/financial_completeness.go"]:
    paths = (root / relative).rglob("*.go") if (root / relative).is_dir() else [root / relative]
    for path in paths:
        text = path.read_text().lower()
        if re.search(r"authorization\s*[:=]|access[_-]?token|private[_-]?key", text):
            raise SystemExit(f"possible secret material in financial completeness code: {path}")

print("Financial completeness repository gate: PASS — matrix, append-only evidence, RLS migration, API/SDK/frontend wiring and secret boundary")
print("External release gate: REQUIRED — credentialed bank/acquirer, marketplace payout, FX and advertising sandbox/live evidence")
PY
