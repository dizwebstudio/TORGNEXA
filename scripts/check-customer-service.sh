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
migration = root / "migrations/000055_customer_service_inbox.sql"
catalog = json.loads((root / "migrations/catalog.json").read_text())
entry = next((item for item in catalog.get("migrations", []) if item.get("version") == 55), None)
digest = hashlib.sha256(migration.read_bytes()).hexdigest()
if not entry or entry.get("file") != migration.name or entry.get("sha256") != digest:
    raise SystemExit("migration catalog/hash drift for version 55")

openapi = (root / "contracts/openapi/torgnexa-v1.yaml").read_text()
for path in [
    "/customer-service/summary:",
    "/customer-service/inbox:",
    "/customer-service/threads/{conversation_id}:",
    "/customer-service/customers/{customer_ref_id}:",
    "/customer-service/inbound:",
    "/customer-service/replies:",
    "/customer-service/assignments:",
    "/customer-service/transitions:",
]:
    if path not in openapi:
        raise SystemExit(f"OpenAPI path missing: {path}")

api = (root / "internal/app/api/customer_service.go").read_text()
repo = (root / "internal/platform/postgres/customerservicerepo/repository.go").read_text()
core = (root / "internal/core/customerservice/service.go").read_text()
frontend = (root / "frontend/src/pages/CustomerServicePage.tsx").read_text()
mcp = (root / "internal/app/mcp/tools.go").read_text() + (root / "internal/app/mcp/server.go").read_text()
for text, required in [
    (api, ["customer_service.read", "Idempotency-Key", "NewInbound", "QueueReply", "Transition"]),
    (repo, ["customer_service_messages", "ON CONFLICT", "set_config('app.organization_id'", "Timeline", "QueueReply"]),
    (core, ["SanitizeText", "IdentityAmbiguous", "BusinessDueAt", "DeliveryUnknown"]),
    (frontend, ["Единый inbox", "Отзывы", "Вопросы", "Внутренняя заметка", "getCustomerServiceThread"]),
    (mcp, ["commerce.customer_service.get", "CustomerServiceReader", "never replies"]),
    ((root / "sdk/typescript/src/client.gen.mjs").read_text(), ["getCustomerServiceSummary", "listCustomerServiceInbox", "queueCustomerServiceReply"]),
]:
    missing = [value for value in required if value not in text]
    if missing:
        raise SystemExit(f"customer service wiring missing: {missing}")

for path in [root / "internal/core/customerservice", root / "internal/platform/postgres/customerservicerepo"]:
    for source in path.rglob("*.go"):
        if source.name.endswith("_test.go"):
            continue
        text = source.read_text().lower()
        if re.search(r"access[_-]?token|private[_-]?key|authorization\s*[:=]|raw[_-]?payload", text):
            raise SystemExit(f"possible secret/raw payload boundary violation: {source}")

sql = migration.read_text().lower()
for forbidden in ["access_token", "private key", "card_number", "raw_payload", "customer_email"]:
    if forbidden in sql:
        raise SystemExit(f"sensitive field {forbidden!r} found in migration")
if sql.count("force row level security") < 9 or "customer_service_messages_no_mutation" not in sql:
    raise SystemExit("customer service migration is missing RLS or append-only controls")

print("Customer service repository gate: PASS — normalized inbox, immutable history, RLS, API/SDK/MCP/frontend and privacy boundary")
print("External release gate: REQUIRED — credentialed channel inbound/reply, attachment and moderation evidence")
PY
