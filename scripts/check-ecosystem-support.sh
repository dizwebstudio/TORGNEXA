#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=local
export GOWORK=off
export TZ=UTC

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$root"

if command -v go >/dev/null 2>&1; then
  go test ./internal/core/ecosystem ./internal/platform/postgres/ecosystemrepo ./internal/app/api ./internal/app/mcp
else
  command -v docker >/dev/null 2>&1 || { echo "ecosystem qualification: Go or Docker is required" >&2; exit 1; }
  docker run --rm -v "$root":/app -w /app golang:1.26.7-bookworm sh -c 'export GOTOOLCHAIN=local GOWORK=off TZ=UTC; go test ./internal/core/ecosystem ./internal/platform/postgres/ecosystemrepo ./internal/app/api ./internal/app/mcp'
fi

python3 - <<'PY'
import hashlib
import json
from pathlib import Path

root = Path.cwd()
sql_path = root / "migrations/000058_ecosystem_support.sql"
sql = sql_path.read_text()
for table in ("ecosystem_onboarding_runs", "ecosystem_partner_certifications"):
    if f"CREATE TABLE {table}" not in sql:
        raise SystemExit(f"ecosystem qualification: missing {table}")
if sql.count("FORCE ROW LEVEL SECURITY") != 2:
    raise SystemExit("ecosystem qualification: both tables must use FORCE RLS")
for invariant in (
    "ecosystem_support_append_only",
    "ecosystem_onboarding_idempotency_uq",
    "ecosystem_partner_certification_idempotency_uq",
    "ecosystem_onboarding_runs_no_mutation",
    "ecosystem_partner_certifications_no_mutation",
):
    if invariant not in sql:
        raise SystemExit(f"ecosystem qualification: missing SQL invariant {invariant}")
if "checks::text !~*" not in sql or "evidence::text !~*" not in sql:
    raise SystemExit("ecosystem qualification: JSON redaction guards are missing")

catalog = json.loads((root / "migrations/catalog.json").read_text())
entry = next((item for item in catalog["migrations"] if item["version"] == 58), None)
if not entry or entry["name"] != "ecosystem_support" or entry["sha256"] != hashlib.sha256(sql_path.read_bytes()).hexdigest():
    raise SystemExit("ecosystem qualification: migration catalog/checksum drift")

core = (root / "internal/core/ecosystem/ecosystem.go").read_text()
for required in ("StatusIntegrated", "StatusVerified", "StatusReady", "StatusQualified", "StatusSupported", "EvaluateOnboarding", "BuildOverview", 'credentialed_sandbox', 'credentialed_live'):
    if required not in core:
        raise SystemExit(f"ecosystem qualification: missing core invariant {required}")
api = (root / "internal/app/api/ecosystem.go").read_text()
for required in ("EcosystemOverviewPath", "EcosystemMetricsPath", "EcosystemOnboardingPath", "EcosystemPartnersPath", "Idempotency-Key", "ecosystem.onboarding.write"):
    if required not in api:
        raise SystemExit(f"ecosystem qualification: missing API invariant {required}")
openapi = (root / "contracts/openapi/torgnexa-v1.yaml").read_text()
for path in ("/ecosystem/overview:", "/ecosystem/metrics:", "/ecosystem/onboarding:", "/ecosystem/partners/certifications:"):
    if path not in openapi:
        raise SystemExit(f"ecosystem qualification: missing OpenAPI path {path}")
for text_path, required in {
    root / "internal/app/mcp/tools.go": ("commerce.ecosystem.overview",),
    root / "internal/app/mcp/server.go": ("commerce.ecosystem.read",),
    root / "frontend/src/pages/EcosystemPage.tsx": ("getEcosystemOverview", "внешние release-gates"),
    root / "frontend/src/shell/navigation.ts": ("/ecosystem", "ecosystem.read"),
    root / "sdk/typescript/src/client.gen.mjs": ("getEcosystemOverview", "createEcosystemOnboarding"),
}.items():
    text = text_path.read_text()
    for value in required:
        if value not in text:
            raise SystemExit(f"ecosystem qualification: {value} missing from {text_path}")

policy = json.loads((root / "architecture/policy.json").read_text())
module_paths = {item["path"] for item in policy["modules"]}
for module in ("internal/core/ecosystem", "internal/platform/postgres/ecosystemrepo"):
    if module not in module_paths:
        raise SystemExit(f"ecosystem qualification: {module} missing from architecture policy")

for relative in (
    "adr/0182-ecosystem-support.md",
    "docs/operations/231-ecosystem-support.md",
    "architecture/reviews/231-ecosystem-support.json",
    "tasks/issues/231-ecosystem-support.md",
):
    if not (root / relative).is_file():
        raise SystemExit(f"ecosystem qualification: missing {relative}")

print("Ecosystem synthetic qualification: PASS — evidence-backed portfolio, onboarding, partner records, RLS, API/SDK/MCP/frontend wiring and explicit status gates")
print("External release gates: REQUIRED — credentialed connectors, partner UAT/rollback, hosted topology/DR, device matrix and production support/SLA evidence")
PY
