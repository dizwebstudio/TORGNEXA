#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=local
export GOWORK=off
export PYTHONDONTWRITEBYTECODE=1
export LC_ALL=C
export TZ=UTC

# py_compile writes bytecode even when PYTHONDONTWRITEBYTECODE is set. Keep
# that verification output outside the repository so a root-owned Docker
# cache cannot make the SDK gate fail with a misleading permission error.
python_cache="$(mktemp -d)"
trap 'rm -rf -- "$python_cache"' EXIT
export PYTHONPYCACHEPREFIX="$python_cache"

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd -- "$repo_root"

go -C tools/sdkgen test ./...
go -C tools/sdkgen run . --root ../.. --check
go -C sdk/go test ./...
go -C sdk/examples/go test ./...
PYTHONPATH=sdk/python python3 -m unittest discover -s sdk/python/tests
python3 -m py_compile sdk/python/torgnexa_sdk/__init__.py sdk/python/torgnexa_sdk/client_gen.py sdk/examples/python/example.py

if ! command -v node >/dev/null 2>&1; then
  echo "node is required to test the generated TypeScript SDK" >&2
  exit 1
fi
node --check sdk/typescript/src/client.gen.mjs
node --check sdk/examples/typescript/example.mjs
node sdk/typescript/test/client.test.mjs
if command -v tsc >/dev/null 2>&1; then
  tsc -p sdk/typescript/tsconfig.json
else
  echo "TypeScript declaration compile skipped: tsc not installed; runtime syntax/tests still passed"
fi

python3 - <<'PY'
from pathlib import Path
import hashlib, json, re

root=Path.cwd()
manifest=json.loads((root/'contracts/sdk/generated-manifest.json').read_text())
source=(root/manifest['openapi']['path']).read_bytes()
actual=hashlib.sha256(source).hexdigest()
if manifest['openapi']['sha256'] != actual:
    raise SystemExit('SDK manifest OpenAPI hash drift')
ops=manifest['operations']
if len(ops) != len({x['operation_id'] for x in ops}):
    raise SystemExit('duplicate operation IDs in generated SDK manifest')
for sdk in manifest['sdks']:
    for rel in sdk['paths']:
        if not (root/rel).is_file():
            raise SystemExit(f'missing generated SDK artifact: {rel}')
for rel in ['sdk/go','sdk/typescript','sdk/python']:
    for path in (root/rel).rglob('*'):
        if not path.is_file() or '__pycache__' in path.parts:
            continue
        text=path.read_text(encoding='utf-8', errors='ignore').lower()
        for forbidden in ('internal/', 'database/sql', 'pgx', 'gorm', 'sqlc'):
            if forbidden in text:
                raise SystemExit(f'{path}: generated/public SDK tree leaks internal model token {forbidden!r}')
openapi=(root/'contracts/openapi/torgnexa-v1.yaml').read_text()
ids=re.findall(r'^\s+operationId:\s*([A-Za-z][A-Za-z0-9]*)\s*$', openapi, re.M)
manifest_ids=[x['operation_id'] for x in ops]
if sorted(ids) != sorted(manifest_ids):
    raise SystemExit('SDK manifest operation inventory differs from OpenAPI operationId inventory')
print(f'SDK boundary check: PASS ({len(ids)} public operations; no internal DB model tokens)')
PY
