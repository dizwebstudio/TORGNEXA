#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

python3 - <<'PY'
import json
from collections import Counter
from pathlib import Path

root = Path.cwd()
manifests = {}
for path in sorted((root / "connectors").rglob("manifest.json")):
    data = json.loads(path.read_text())
    manifests[data["id"]] = path

matrix = json.loads((root / "contracts/connectors/readiness-matrix-v1.json").read_text())
support = json.loads((root / "contracts/connectors/builtin-runtime-support-v1.json").read_text())
profiles = matrix.get("profiles", [])
support_by_id = {item["connector_id"]: item for item in support["connectors"]}

if len(manifests) != 61 or len(profiles) != 61:
    raise SystemExit(f"readiness inventory mismatch: manifests={len(manifests)} profiles={len(profiles)}")
profile_by_id = {item["connector_id"]: item for item in profiles}
if set(manifests) != set(profile_by_id) or set(manifests) != set(support_by_id):
    raise SystemExit("manifest, readiness and runtime-support IDs differ")
if len(profile_by_id) != len(profiles):
    raise SystemExit("duplicate connector in readiness matrix")

allowed = {"manifest_only", "health_only", "read_only", "partially_supported", "ready", "qualified", "degraded", "reauthorization_required", "not_available"}
for connector_id, profile in profile_by_id.items():
    required = ("status", "owner", "priority", "decision", "next_action", "last_verified_at", "conformance_ref", "runtime_ref")
    if any(not profile.get(field) for field in required):
        raise SystemExit(f"{connector_id}: incomplete readiness profile")
    if profile["status"] not in allowed:
        raise SystemExit(f"{connector_id}: invalid status {profile['status']!r}")
    if profile["status"] == "qualified" and profile.get("live_qualification_status") not in {"passed", "qualified"}:
        raise SystemExit(f"{connector_id}: qualified without retained live evidence")
    if profile.get("health_only") and profile["status"] != "health_only":
        raise SystemExit(f"{connector_id}: health-only connector promoted to {profile['status']}")
    report_path = root / profile["conformance_ref"]
    report = json.loads(report_path.read_text())
    if report.get("connector_id") != connector_id or report.get("passed") is not True:
        raise SystemExit(f"{connector_id}: conformance report is not passed")
    check_ids = {check.get("id") for check in report.get("checks", []) if check.get("status") == "pass"}
    required_checks = {"manifest_sdk", "auth_boundary", "health_normalization", "normalized_errors", "rate_limit_retry", "idempotency", "tenant_isolation", "dry_run_side_effect_suppression", "production_credential_rejection", "egress_grant_enforcement", "resource_limit_failure", "sandbox_isolation"}
    if not required_checks.issubset(check_ids):
        raise SystemExit(f"{connector_id}: conformance admission checks are incomplete")

explicit_health_only = sum(1 for item in support["connectors"] if item.get("health_only"))
if explicit_health_only != 16:
    raise SystemExit(f"runtime support health-only count changed unexpectedly: {explicit_health_only}")

counts = Counter(item["status"] for item in profiles)
print("Connector readiness gate: PASS")
print(f"Inventory: {len(manifests)} manifests / {len(profiles)} profiles")
print("Statuses: " + ", ".join(f"{key}={counts.get(key, 0)}" for key in sorted(allowed)))
print("Health-only source count: 16 (two zero-operation specialized surfaces are manifest_only)")
print("Credentialed sandbox/live qualification remains an external release gate; no unverified connector is promoted to qualified.")
PY
