#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "connector conformance reference qualification: SKIP (Linux isolation reference only)"
  exit 0
fi
if ! command -v unshare >/dev/null 2>&1; then
  echo "connector conformance reference qualification: FAIL (unshare missing)" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cd "$ROOT"
CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "$TMP/torgnexa-connector-emulator" ./cmd/torgnexa-connector-emulator
CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "$TMP/torgnexa-connector-conformance" ./cmd/torgnexa-connector-conformance
TORGNEXA_PRODUCTION_SECRET="must-not-cross-conformance-sandbox" \
  "$TMP/torgnexa-connector-conformance" -emulator "$TMP/torgnexa-connector-emulator" > "$TMP/report.json"

python3 - "$TMP/report.json" <<'PY'
import json, pathlib, sys
path = pathlib.Path(sys.argv[1])
data = json.loads(path.read_text())
required = [
    "manifest_sdk",
    "auth_boundary",
    "health_normalization",
    "normalized_errors",
    "rate_limit_retry",
    "idempotency",
    "webhook_replay",
    "tenant_isolation",
    "dry_run_side_effect_suppression",
    "production_credential_rejection",
    "egress_grant_enforcement",
    "resource_limit_failure",
    "sandbox_isolation",
]
assert data["suite_version"] == 1
assert data["sdk_version"] == 1
assert data["passed"] is True
assert [item["id"] for item in data["checks"]] == required
assert all(item["status"] == "pass" and "reason_code" not in item for item in data["checks"])
text = path.read_text()
for forbidden in ("must-not-cross-conformance-sandbox", "Authorization", "Bearer ", "refresh_token", "client_secret"):
    assert forbidden not in text
print(f"connector conformance reference qualification: PASS ({len(required)} required checks)")
PY
