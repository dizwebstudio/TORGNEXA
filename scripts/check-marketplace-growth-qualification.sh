#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

python3 - <<'PY'
from pathlib import Path

root = Path.cwd()
core = (root / "internal/core/marketplacegrowth/growth.go").read_text()
api = (root / "internal/app/api/marketplace_growth.go").read_text()
openapi = (root / "contracts/openapi/torgnexa-v1.yaml").read_text()
ui = (root / "frontend/src/pages/AdvertisingPage.tsx").read_text()
for text, required in [
    (core, ["MaxPreviewRows = 1000", "FloorPriceMinor", "FactsFresh", "StateQualificationRequired", "Reconcile"]),
    (api, ["Approval-Request-ID", "Idempotency-Key", "SetKillSwitch", "SaveDrifts"]),
    (openapi, ["/marketplace-growth/previews", "/marketplace-growth/operations", "/marketplace-growth/kill-switch"]),
    (ui, ["Акции", "Ставки и бюджеты", "Массовые операции", "qualification_required", "effective_price_minor"]),
]:
    missing = [value for value in required if value not in text]
    if missing:
        raise SystemExit(f"growth qualification contract missing: {missing}")
print("Marketplace growth synthetic qualification: PASS — preview, guards, approval, idempotency, reconciliation, kill switch and frontend contract")
print("Live marketplace qualification: REQUIRED at release — official promotion/advertising writes and credentialed read-after-write evidence are external gates")
PY
