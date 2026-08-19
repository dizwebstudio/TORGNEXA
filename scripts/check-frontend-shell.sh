#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

command -v node >/dev/null
command -v tsc >/dev/null

node_major="$(node -p 'Number(process.versions.node.split(".")[0])')"
if [[ "$node_major" -lt 22 ]]; then
  echo "frontend-check: Node 22+ is required for repository validation" >&2
  exit 1
fi

tmp="frontend/.repository-test"
rm -rf "$tmp"
trap 'rm -rf "$tmp"' EXIT

tsc -p frontend/tsconfig.logic.json
node --test frontend/test/*.test.mjs
tsc -p frontend/tsconfig.repository.json
python3 scripts/generate-frontend-connector-catalog.py --check

python3 - <<'PY'
import json
from pathlib import Path

root = Path('.')
pkg = json.loads((root/'frontend/package.json').read_text())
if pkg.get('packageManager') != 'npm@10.9.2':
    raise SystemExit('frontend-check: packageManager must be npm@10.9.2')
required = {
    'react': '19.2.8',
    'react-dom': '19.2.8',
    '@tanstack/react-query': '5.101.4',
    '@torgnexa/sdk': 'file:../sdk/typescript',
}
required_dev = {
    '@types/node': '22.16.0',
    '@types/react': '19.2.18',
    '@types/react-dom': '19.2.4',
    '@vitejs/plugin-react': '6.0.5',
    'typescript': '7.0.2',
    'vite': '8.0.16',
}
for name, version in required.items():
    if pkg.get('dependencies', {}).get(name) != version:
        raise SystemExit(f'frontend-check: dependency {name} must be pinned to {version}')
for name, version in required_dev.items():
    if pkg.get('devDependencies', {}).get(name) != version:
        raise SystemExit(f'frontend-check: dev dependency {name} must be pinned to {version}')

source_files = [p for p in sorted((root/'frontend/src').rglob('*')) if p.is_file()]
source = '\n'.join(p.read_text(errors='strict') for p in source_files)
handwritten_source = '\n'.join(p.read_text(errors='strict') for p in source_files if 'generated' not in p.relative_to(root/'frontend/src').parts)
lower = source.lower()
handwritten_lower = handwritten_source.lower()
for forbidden in ('localstorage', 'sessionstorage', 'document.cookie', 'organization_id', 'workspace_id'):
    if forbidden in lower:
        raise SystemExit(f'frontend-check: forbidden browser/session or tenant selector token: {forbidden}')
for provider in ('wildberries', 'ozon', 'yandex_market', 'aliexpress', 'avito', 'megamarket'):
    if provider in handwritten_lower:
        raise SystemExit(f'frontend-check: provider-specific branch/token is forbidden in shell source: {provider}')
if 'from "@torgnexa/sdk"' not in source:
    raise SystemExit('frontend-check: frontend must consume the generated TypeScript SDK package')
if 'window.__TORGNEXA_AUTH_ADAPTER__' not in source:
    raise SystemExit('frontend-check: host-owned OIDC adapter boundary is missing')

copied = list((root/'frontend').rglob('client.gen.*'))
if copied:
    raise SystemExit('frontend-check: generated SDK files must not be copied into frontend: '+', '.join(map(str,copied)))
print('Frontend static policy: PASS')
PY

if [[ -d frontend/node_modules ]]; then
  npm --prefix frontend run build
else
  echo "frontend-check: node_modules absent; frontend remains source-only and cannot enter a production JS release without a committed lockfile"
fi

echo "Frontend shell validation: PASS"
