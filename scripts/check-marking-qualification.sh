#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=local
export GOWORK=off
export LC_ALL=C
export TZ=UTC

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

# Deterministic repository qualification only. This does not claim live
# Chestny ZNAK/EDO readiness and never requires production credentials.
go test ./internal/core/marking ./internal/platform/connectors ./internal/platform/marking ./internal/platform/postgres/markingrepo ./internal/app/api

python3 - <<'PY'
from pathlib import Path

root = Path('.')
sql = (root / 'migrations/000037_marking_execution.sql').read_text()
required_tables = [
    'marking_code_batches', 'marking_codes', 'marking_operations',
    'marking_packages', 'marking_package_links', 'marking_print_jobs',
    'marking_scans', 'marking_documents', 'marking_document_lines',
    'marking_remote_observations', 'marking_drifts', 'marking_process_runs',
]
for table in required_tables:
    if f'CREATE TABLE {table}' not in sql:
        raise SystemExit(f'marking qualification: missing table {table}')
if sql.count('FORCE ROW LEVEL SECURITY') != len(required_tables):
    raise SystemExit('marking qualification: every marking table must use FORCE RLS')
for forbidden in ('raw_code', 'raw_codes', 'code_value', 'plaintext_code'):
    if forbidden in sql.lower():
        raise SystemExit(f'marking qualification: raw code column leaked into SQL: {forbidden}')

sdk = (root / 'internal/platform/connectors/marking.go').read_text()
capabilities = (root / 'internal/platform/connectors/capabilities.go').read_text()
for capability in (
    'marking.codes.request', 'marking.codes.reserve',
    'marking.aggregation.write', 'marking.circulation.introduce',
    'marking.circulation.withdraw', 'marking.transfer.write',
):
    if capability not in capabilities:
        raise SystemExit(f'marking qualification: missing typed capability {capability}')
for required in ('DryRun', 'MarkingUnknown', 'IdempotencyKey', 'MarkingAggregationWriter', 'MarkingTransferWriter'):
    if required not in sdk:
        raise SystemExit(f'marking qualification: missing SDK invariant {required}')

core = (root / 'internal/core/marking/marking.go').read_text()
for required in ('CodeFingerprint', 'ValidatePackageTree', 'PrintUseRegistry', 'RawCodeStore'):
    if required not in core:
        raise SystemExit(f'marking qualification: missing domain invariant {required}')
print('Marking synthetic qualification: PASS — raw-code boundary, 12 RLS tables, typed writes, package graph and one-use print guard')
print('Live provider qualification: NOT CLAIMED — official non-production fixtures and legal credentials are required')
PY
