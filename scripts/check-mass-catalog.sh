#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=local
export GOWORK=off
export TZ=UTC

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$root"

if command -v go >/dev/null 2>&1; then
  go test ./internal/core/catalogbulk ./internal/platform/postgres/catalogbulkrepo ./internal/app/api ./internal/app/mcp
else
  command -v docker >/dev/null 2>&1 || { echo "mass catalog qualification: Go or Docker is required" >&2; exit 1; }
  docker run --rm -v "$root":/app -w /app golang:1.26.7-bookworm sh -c 'export GOTOOLCHAIN=local GOWORK=off TZ=UTC; go test ./internal/core/catalogbulk ./internal/platform/postgres/catalogbulkrepo ./internal/app/api ./internal/app/mcp'
fi

python3 - <<'PY'
import hashlib
import json
from pathlib import Path

root = Path('.')
sql_path = root / 'migrations/000057_mass_catalog_management.sql'
sql = sql_path.read_text()
for table in ('catalog_bulk_previews', 'catalog_bulk_runs', 'catalog_bulk_kill_switches'):
    if f'CREATE TABLE {table}' not in sql:
        raise SystemExit(f'mass catalog qualification: missing {table}')
if sql.count('FORCE ROW LEVEL SECURITY') != 3:
    raise SystemExit('mass catalog qualification: every bulk table must use FORCE RLS')
for required in ('catalog_bulk_workspace_no_mutation', 'catalog_bulk_run_idempotency_uq', 'catalog_bulk_run_preview_fk', 'catalog_bulk_kill_switches_no_update_delete'):
    if required not in sql:
        raise SystemExit(f'mass catalog qualification: missing SQL invariant {required}')
if 'preview_document::text !~*' not in sql or 'run_document::text !~*' not in sql:
    raise SystemExit('mass catalog qualification: JSON evidence redaction guards are missing')

catalog = json.loads((root / 'migrations/catalog.json').read_text())
migration = next(item for item in catalog['migrations'] if item['version'] == 57)
checksum = hashlib.sha256(sql_path.read_bytes()).hexdigest()
if migration['name'] != 'mass_catalog_management' or migration['sha256'] != checksum:
    raise SystemExit('mass catalog qualification: migration catalog/checksum drift')

core = (root / 'internal/core/catalogbulk/bulk.go').read_text()
for required in ('MaxSKUs = 1000', 'BuildPreview', 'NewRun', 'Reconcile', 'CapabilityQualificationNeeded', 'ErrKillSwitch'):
    if required not in core:
        raise SystemExit(f'mass catalog qualification: missing core invariant {required}')
api = (root / 'internal/app/api/catalog_bulk.go').read_text()
for required in ('CatalogBulkPreviewPath', 'CatalogBulkApplyPath', 'ListPreviews', 'ListRuns', 'KillSwitch', 'Approval-Request-ID'):
    if required not in api:
        raise SystemExit(f'mass catalog qualification: missing API invariant {required}')
openapi = (root / 'contracts/openapi/torgnexa-v1.yaml').read_text()
for path in ('/catalog/bulk/summary:', '/catalog/bulk/previews:', '/catalog/bulk/runs:', '/catalog/bulk/runs/{run_id}/reconcile:', '/catalog/bulk/kill-switch:'):
    if path not in openapi:
        raise SystemExit(f'mass catalog qualification: missing OpenAPI path {path}')
events = json.loads((root / 'contracts/events/event-catalog.json').read_text())['events']
event_names = {item['event_type'] for item in events}
for event in ('commerce.catalog.bulk_preview_created.v1', 'commerce.catalog.bulk_run_queued.v1', 'commerce.catalog.bulk_row_reconciled.v1'):
    if event not in event_names:
        raise SystemExit(f'mass catalog qualification: missing event {event}')
for path in ('frontend/src/pages/MassCatalogPage.tsx', 'docs/operations/230-mass-catalog-management.md', 'adr/0181-mass-catalog-management.md'):
    if not (root / path).exists():
        raise SystemExit(f'mass catalog qualification: missing {path}')
page = (root / 'frontend/src/pages/MassCatalogPage.tsx').read_text()
for required in ('Capability matrix', 'Before / after diff', 'Согласовать и применить', 'Read-after-write observation'):
    if required not in page:
        raise SystemExit(f'mass catalog qualification: missing UI surface {required}')
mcp = (root / 'internal/app/mcp/server.go').read_text()
if 'commerce.catalog.bulk.preview' not in mcp or 'http_approval_only' not in (root / 'internal/app/mcp/tools.go').read_text():
    raise SystemExit('mass catalog qualification: MCP dry-run boundary missing')
print('Mass catalog synthetic qualification: PASS — bounded 1,000-SKU scope, typed preview/diff, quality/capability guards, approval-bound cursor history, RLS, kill switch, MCP dry-run and frontend workspace')
print('Live channel qualification: REQUIRED at release — official taxonomy, field/media/price evidence and credentialed remote read-after-write remain external')
PY
