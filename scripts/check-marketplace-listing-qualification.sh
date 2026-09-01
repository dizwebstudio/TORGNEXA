#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=local
export GOWORK=off
export TZ=UTC

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$root"

# Repository qualification is deterministic. When the host does not have Go,
# use the pinned project toolchain image; no provider credential is required.
if command -v go >/dev/null 2>&1; then
  go test ./internal/core/marketplacelisting ./internal/platform/connectors ./internal/platform/postgres/marketplacelistingrepo ./internal/app/api ./internal/app/mcp
else
  command -v docker >/dev/null 2>&1 || { echo "marketplace listing qualification: Go or Docker is required" >&2; exit 1; }
  docker run --rm -v "$root":/app -w /app golang:1.26.7-alpine3.23 sh -c 'export GOTOOLCHAIN=local GOWORK=off TZ=UTC; go test ./internal/core/marketplacelisting ./internal/platform/connectors ./internal/platform/postgres/marketplacelistingrepo ./internal/app/api ./internal/app/mcp'
fi

python3 - <<'PY'
from pathlib import Path

root = Path('.')
sql = (root / 'migrations/000052_marketplace_listing_workspace.sql').read_text()
for table in ('marketplace_listing_taxonomies', 'marketplace_listing_batches'):
    if f'CREATE TABLE {table}' not in sql:
        raise SystemExit(f'marketplace listing qualification: missing {table}')
if sql.count('FORCE ROW LEVEL SECURITY') != 2:
    raise SystemExit('marketplace listing qualification: every table must use FORCE RLS')
if 'batch_idempotency_uq' not in sql or 'no_update_delete' not in sql:
    raise SystemExit('marketplace listing qualification: append-only/idempotency guards missing')
for forbidden in ('authorization', 'access_token', 'client_secret', 'raw_payload'):
    if forbidden in sql.lower():
        raise SystemExit(f'marketplace listing qualification: sensitive marker leaked into SQL: {forbidden}')

core = (root / 'internal/core/marketplacelisting/listing.go').read_text()
for required in ('RequirementConditional', 'ComputeFingerprint', 'BuildBatchPreview', 'MaxBatchItems', 'Reconcile'):
    if required not in core:
        raise SystemExit(f'marketplace listing qualification: missing core invariant {required}')
sdk = (root / 'internal/platform/connectors/marketplace_listing.go').read_text()
for required in ('MarketplaceListingWriter', 'MarketplaceListingStatusReader', 'DryRun', 'IdempotencyKey'):
    if required not in sdk:
        raise SystemExit(f'marketplace listing qualification: missing SDK invariant {required}')
openapi = (root / 'contracts/openapi/torgnexa-v1.yaml').read_text()
for path in ('/marketplace-listings/taxonomy:', '/marketplace-listings/batch/preview:', '/marketplace-listings/batch/apply:', '/marketplace-listings/read-after-write:'):
    if path not in openapi:
        raise SystemExit(f'marketplace listing qualification: missing OpenAPI path {path}')
if not (root / 'frontend/src/pages/MarketplaceListingPage.tsx').exists():
    raise SystemExit('marketplace listing qualification: frontend workspace is missing')
print('Marketplace listing synthetic qualification: PASS — taxonomy, conditional attributes, bounded 1000-SKU preview, approval journal, UI and read-after-write contracts')
print('Live marketplace qualification: REQUIRED at release — official taxonomy, remote batch write and credentialed read-after-write evidence are not synthesized')
PY
