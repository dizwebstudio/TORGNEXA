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
migration = root / "migrations/000056_mobile_warehouse_fulfillment.sql"
catalog = json.loads((root / "migrations/catalog.json").read_text())
entry = next((item for item in catalog.get("migrations", []) if item.get("version") == 56), None)
digest = hashlib.sha256(migration.read_bytes()).hexdigest()
if not entry or entry.get("file") != migration.name or entry.get("sha256") != digest:
    raise SystemExit("migration catalog/hash drift for version 56")

openapi = (root / "contracts/openapi/torgnexa-v1.yaml").read_text()
for path in [
    "/warehouse-mobile/summary:", "/warehouse-mobile/plans:", "/warehouse-mobile/plans/{plan_id}:",
    "/warehouse-mobile/plans/{plan_id}/advance:", "/warehouse-mobile/plans/{plan_id}/pick-batches:",
    "/warehouse-mobile/batches:", "/warehouse-mobile/scans:", "/warehouse-mobile/packs:",
    "/warehouse-mobile/packs/{pack_id}/close:", "/warehouse-mobile/print-jobs:",
    "/warehouse-mobile/print-jobs/{print_job_id}/status:", "/warehouse-mobile/devices:",
    "/warehouse-mobile/devices/{device_id}/revoke:", "/warehouse-mobile/offline-intents:",
    "/warehouse-mobile/observations:",
]:
    if path not in openapi:
        raise SystemExit(f"OpenAPI path missing: {path}")
for operation in [
    "getWarehouseMobileSummary", "listWarehouseMobilePlans", "createWarehouseMobilePlan", "getWarehouseMobilePlan",
    "advanceWarehouseMobilePlan", "createWarehouseMobilePickBatchForPlan", "listWarehouseMobilePickBatches",
    "createWarehouseMobilePickBatch", "recordWarehouseMobileScan", "listWarehouseMobilePacks", "createWarehouseMobilePack",
    "closeWarehouseMobilePack", "listWarehouseMobilePrintJobs", "queueWarehouseMobilePrint",
    "recordWarehouseMobilePrintStatus", "listWarehouseMobileDevices", "registerWarehouseMobileDevice",
    "revokeWarehouseMobileDevice", "listWarehouseMobileOfflineIntents", "replayWarehouseMobileOfflineScan",
    "recordWarehouseMobileRemoteObservation",
]:
    if f"operationId: {operation}" not in openapi:
        raise SystemExit(f"OpenAPI operation missing: {operation}")

api = (root / "internal/app/api/mobile_warehouse.go").read_text()
repo = (root / "internal/platform/postgres/inventoryrepo/mobile.go").read_text()
core = (root / "internal/core/inventory/mobile.go").read_text()
frontend = (root / "frontend/src/pages/MobileWarehousePage.tsx").read_text()
shell = (root / "frontend/src/shell/AppShell.tsx").read_text()
navigation = (root / "frontend/src/shell/navigation.ts").read_text()
events = json.loads((root / "contracts/events/event-catalog.json").read_text())
event_types = {item.get("event_type") for item in events.get("events", [])}
required = {
    "api": ["newMobileWarehouseRoutes", "Idempotency-Key", "wms.write", "AdvanceMobilePlan", "RecordMobileScan", "ListMobilePacks"],
    "repo": ["mobile_fulfillment_plans", "mobile_scan_evidence", "mobile_print_jobs", "mobile_offline_intents", "FOR UPDATE", "idempotency_key"],
    "core": ["FulfillmentFBS", "FulfillmentFBO", "MobileLocalOperationAllowed", "MobileCodeDigest", "PackageFacts"],
    "frontend": ["Мобильный склад", "FBS", "FBO", "Сканирование", "Упаковка", "Печать", "Офлайн", "replayWarehouseMobileOfflineScan"],
    "shell": ["MobileWarehousePage", "/warehouse/mobile"],
    "navigation": ["mobile-warehouse", "warehouse", "stock.read"],
}
for name, text in [("api", api), ("repo", repo), ("core", core), ("frontend", frontend), ("shell", shell), ("navigation", navigation)]:
    missing = [value for value in required[name] if value not in text]
    if missing:
        raise SystemExit(f"mobile warehouse wiring missing in {name}: {missing}")

expected_events = {
    "commerce.fulfillment.mobile_print_job_changed.v1",
    "commerce.fulfillment.mobile_scan_recorded.v1",
    "commerce.fulfillment.mobile_task_changed.v1",
}
if not expected_events.issubset(event_types):
    raise SystemExit(f"mobile event catalog missing: {sorted(expected_events - event_types)}")
for event in expected_events:
    schema_name = next(item["payload_schema"].split("/", 1)[1] for item in events["events"] if item["event_type"] == event)
    schema = root / "contracts/events" / schema_name
    if not schema.exists():
        raise SystemExit(f"mobile event schema missing: {schema}")

sql = migration.read_text().lower()
for forbidden in ["access_token", "private_key", "authorization", "raw_payload", "raw_barcode", "raw_code"]:
    if re.search(rf"\b{re.escape(forbidden)}\b", sql):
        raise SystemExit(f"sensitive/raw field {forbidden!r} found in migration")
if sql.count("force row level security") < 9 or "mobile_scan_evidence_no_mutation" not in sql or "mobile_remote_observations_no_mutation" not in sql:
    raise SystemExit("mobile migration is missing forced RLS or append-only controls")
if "code_digest" not in sql or re.search(r"\b(?:barcode|raw_code)\s+(?:text|varchar|json)", sql):
    raise SystemExit("mobile scan storage must be digest-only")

for generated, symbols in [
    (root / "sdk/typescript/src/client.gen.mjs", ["advanceWarehouseMobilePlan", "recordWarehouseMobileScan", "replayWarehouseMobileOfflineScan"]),
    (root / "sdk/python/torgnexa_sdk/client_gen.py", ["advance_warehouse_mobile_plan", "record_warehouse_mobile_scan"]),
    (root / "sdk/go/torgnexa/client.gen.go", ["AdvanceWarehouseMobilePlan", "RecordWarehouseMobileScan"]),
]:
    text = generated.read_text()
    missing = [symbol for symbol in symbols if symbol not in text]
    if missing:
        raise SystemExit(f"generated SDK symbols missing in {generated}: {missing}")

print("Mobile warehouse repository gate: PASS — FBS/FBO plan policy, WMS-backed mobile work, digest-only scan, pack/print, offline receipts, RLS, events, SDK and frontend")
print("External release gate: REQUIRED — credentialed FBS/FBO/carrier and scanner/camera/scale/printer qualification with retained redacted evidence")
PY
