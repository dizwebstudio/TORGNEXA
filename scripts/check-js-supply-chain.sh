#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
policy="$repo_root/supply-chain/js-artifacts.json"
mode="${1:-repository}"
[[ "$mode" == repository || "$mode" == release ]] || { echo "usage: $0 [repository|release]" >&2; exit 2; }

for cmd in jq sha512sum; do command -v "$cmd" >/dev/null || { echo "js-supply-chain: missing $cmd" >&2; exit 1; }; done
[[ -f "$policy" && ! -L "$policy" ]] || { echo "js-supply-chain: missing policy" >&2; exit 1; }

python3 - "$repo_root" "$policy" "$mode" <<'PY'
import json, re, sys
from pathlib import Path
root=Path(sys.argv[1]); policy=json.loads(Path(sys.argv[2]).read_text()); mode=sys.argv[3]
registered={str(Path(item['path'])/'package.json') for item in policy['artifacts']}
actual={str(p.relative_to(root)) for p in root.rglob('package.json') if 'node_modules' not in p.parts}
if actual != registered:
    missing=sorted(actual-registered); stale=sorted(registered-actual)
    raise SystemExit(f"js-supply-chain: package registry drift unregistered={missing} missing={stale}")
release_inventory=(root/'supply-chain/release-artifacts.json').read_text()
release_builder=(root/'scripts/build-release.sh').read_text()
exact=re.compile(r'^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$')
allowed_file=re.compile(r'^file:\.\.?/')
for item in policy['artifacts']:
    p=root/item['path']/'package.json'
    if not p.is_file(): raise SystemExit(f"js-supply-chain: missing {p.relative_to(root)}")
    pkg=json.loads(p.read_text())
    if pkg.get('packageManager') != policy['package_manager']:
        raise SystemExit(f"js-supply-chain: {item['name']} packageManager must be {policy['package_manager']}")
    for section in ('dependencies','devDependencies','peerDependencies','optionalDependencies'):
        for name,spec in pkg.get(section,{}).items():
            if spec in ('*','latest') or any(c in spec for c in '^~><| '):
                raise SystemExit(f"js-supply-chain: floating dependency {item['name']} {section} {name}={spec}")
            if not (exact.match(spec) or allowed_file.match(spec)):
                raise SystemExit(f"js-supply-chain: unsupported dependency spec {item['name']} {name}={spec}")
    lock_name=item.get('lockfile')
    lock=(root/lock_name) if lock_name else None
    enabled=bool(item.get('release_enabled'))
    if not enabled and mode == 'release':
        # A source-only package must not be smuggled into the current Go release inventory/builder.
        if item['path'] in release_inventory or item['path'] in release_builder:
            raise SystemExit(f"js-supply-chain: source-only artifact {item['name']} is referenced by production release machinery")
    if enabled:
        if not lock or not lock.is_file() or lock.is_symlink():
            raise SystemExit(f"js-supply-chain: release-enabled artifact {item['name']} requires committed lockfile")
    if lock and lock.is_file():
        data=json.loads(lock.read_text())
        if data.get('lockfileVersion') != 3: raise SystemExit(f"js-supply-chain: {lock_name} must be lockfileVersion 3")
        rootpkg=data.get('packages',{}).get('',{})
        if rootpkg.get('name') != pkg.get('name') or rootpkg.get('version') != pkg.get('version'):
            raise SystemExit(f"js-supply-chain: {lock_name} root identity drift")
        for section in ('dependencies','devDependencies','peerDependencies','optionalDependencies'):
            if rootpkg.get(section,{}) != pkg.get(section,{}):
                raise SystemExit(f"js-supply-chain: {lock_name} root {section} drift")
        for path, entry in data.get('packages',{}).items():
            if not path: continue
            resolved=entry.get('resolved','')
            integrity=entry.get('integrity','')
            if resolved and not (resolved.startswith('https://registry.npmjs.org/') or resolved.startswith('../')):
                raise SystemExit(f"js-supply-chain: non-registry resolved URL in {lock_name}: {resolved}")
            if resolved.startswith('https://registry.npmjs.org/') and not integrity.startswith('sha512-'):
                raise SystemExit(f"js-supply-chain: package without sha512 integrity in {lock_name}: {path}")
print(f"JS supply-chain policy ({mode}): PASS")
PY

# Additional invariant for the publishable n8n package: the intentionally minimal graph is compiler-only.
n8n_lock="$repo_root/integrations/n8n-nodes-torgnexa/package-lock.json"
[[ "$(jq -r '.packages["node_modules/typescript"].version' "$n8n_lock")" == "5.9.3" ]] || { echo "js-supply-chain: TypeScript lock drift" >&2; exit 1; }
[[ "$(jq -r '.packages["node_modules/typescript"].integrity' "$n8n_lock")" == "sha512-jl1vZzPDinLr9eUt3J/t7V6FgNEw9QjvBPdysz9KfQDD41fQrC2Y4vKQdiaUpFT4bXlb1RHhLpp8wtm6M5TgSw==" ]] || { echo "js-supply-chain: TypeScript integrity drift" >&2; exit 1; }
[[ "$(jq '.packages | keys | length' "$n8n_lock")" == "2" ]] || { echo "js-supply-chain: unexpected n8n build dependency graph expansion" >&2; exit 1; }

echo "JS supply-chain lock verification: PASS"
