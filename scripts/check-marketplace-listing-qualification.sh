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
  go test ./internal/core/marketplacelisting ./internal/core/marketplacepublication ./internal/platform/connectors ./internal/platform/marketplacetaxonomy ./internal/platform/postgres/marketplacelistingrepo ./internal/platform/postgres/marketplacepublicationrepo ./internal/app/api ./internal/app/worker ./internal/app/mcp ./connectors/marketplaces/wildberries ./connectors/marketplaces/ozon ./connectors/marketplaces/yandex-market
else
  command -v docker >/dev/null 2>&1 || { echo "marketplace listing qualification: Go or Docker is required" >&2; exit 1; }
  docker run --rm -v "$root":/app -w /app golang:1.26.7-alpine3.23 sh -c 'export GOTOOLCHAIN=local GOWORK=off TZ=UTC; go test ./internal/core/marketplacelisting ./internal/core/marketplacepublication ./internal/platform/connectors ./internal/platform/marketplacetaxonomy ./internal/platform/postgres/marketplacelistingrepo ./internal/platform/postgres/marketplacepublicationrepo ./internal/app/api ./internal/app/worker ./internal/app/mcp ./connectors/marketplaces/wildberries ./connectors/marketplaces/ozon ./connectors/marketplaces/yandex-market'
fi

python3 - <<'PY'
from pathlib import Path

root = Path('.')
evidence_schema = root / 'contracts/qualification/marketplace-remote-evidence-v1.schema.json'
if not evidence_schema.is_file():
    raise SystemExit('marketplace listing qualification: marketplace evidence schema is missing')
try:
    evidence_schema_document = __import__('json').loads(evidence_schema.read_text())
except ValueError as exc:
    raise SystemExit(f'marketplace listing qualification: invalid evidence schema: {exc}')
if evidence_schema_document.get('$id', '').endswith('marketplace-remote-evidence-v1.schema.json') is False or evidence_schema_document.get('properties', {}).get('schema_version', {}).get('const') != 1:
    raise SystemExit('marketplace listing qualification: marketplace evidence schema identity/version drift')
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
for required in ('MarketplaceListingTaxonomyReader', 'MarketplaceListingWriter', 'MarketplaceListingStatusReader', 'DryRun', 'IdempotencyKey'):
    if required not in sdk:
        raise SystemExit(f'marketplace listing qualification: missing SDK invariant {required}')
taxonomy = (root / 'internal/platform/marketplacetaxonomy/profile.go').read_text()
for required in ('ProviderTaxonomyProfile', 'RemoteOperationAdmission', 'ErrRemoteOperationNotQualified'):
    if required not in taxonomy:
        raise SystemExit(f'marketplace listing qualification: missing provider admission invariant {required}')
openapi = (root / 'contracts/openapi/torgnexa-v1.yaml').read_text()
for path in ('/marketplace-listings/taxonomy:', '/marketplace-listings/batch/preview:', '/marketplace-listings/batch/apply:', '/marketplace-listings/read-after-write:'):
    if path not in openapi:
        raise SystemExit(f'marketplace listing qualification: missing OpenAPI path {path}')
manifest = __import__('json').loads((root / 'contracts/sdk/generated-manifest.json').read_text())
if __import__('hashlib').sha256((root / manifest['openapi']['path']).read_bytes()).hexdigest() != manifest['openapi']['sha256']:
    raise SystemExit('marketplace listing qualification: generated SDK OpenAPI hash drift')
if 'remote_operation_ids' not in openapi or 'remote_operation_id' not in openapi:
    raise SystemExit('marketplace listing qualification: asynchronous operation identity is missing')
api = (root / 'internal/app/api/marketplace_listing.go').read_text()
for required in ('RemoteOperationID', 'validRemotePublicationIdentity'):
    if required not in api:
        raise SystemExit(f'marketplace listing qualification: missing batch identity invariant {required}')
repo = (root / 'internal/platform/postgres/marketplacepublicationrepo/repository.go').read_text()
if 'EnqueueBatch' not in repo or 'remote_id,remote_operation_id,attempt' not in repo:
    raise SystemExit('marketplace listing qualification: durable operation identity is not persisted')
if not (root / 'frontend/src/pages/MarketplaceListingPage.tsx').exists():
    raise SystemExit('marketplace listing qualification: frontend workspace is missing')
print('Marketplace listing synthetic qualification: PASS — taxonomy, conditional attributes, bounded 1000-SKU preview, approval journal, UI and read-after-write contracts')
print('Live marketplace qualification: REQUIRED at release — official taxonomy, remote batch write and credentialed read-after-write evidence are not synthesized')
PY
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$root/scripts" python3 "$root/scripts/marketplace_remote_evidence.py" \
  --input "$root/qualification/marketplace-remote-qualification.example.json" --scope listing >/dev/null
