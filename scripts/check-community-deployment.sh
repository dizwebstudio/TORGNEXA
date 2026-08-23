#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"
required=(postgres keycloak-db-init migrate app-db-role kafka valkey clickhouse garage-config garage keycloak api worker scheduler mcp)
for svc in "${required[@]}"; do
  grep -Eq "^  ${svc}:$" docker-compose.yml || { echo "missing compose service: $svc" >&2; exit 1; }
done
for f in Dockerfile .dockerignore deploy/postgres/migrate.sh deploy/postgres/configure-app-role.sh deploy/postgres/rebaseline-pre-v1.sh deploy/postgres/catalog.tsv deploy/postgres/legacy_pre_v1_catalog.tsv deploy/postgres/legacy_pre_v1_catalog.sha256 deploy/keycloak/init-db.sh deploy/keycloak/torgnexa-realm.json deploy/garage/render-config.sh; do
  [[ -f "$f" && ! -L "$f" ]] || { echo "missing/unsafe deployment file: $f" >&2; exit 1; }
done
if grep -Eq 'image:[[:space:]]+[^#[:space:]]*:latest([[:space:]]|$)' docker-compose.yml; then
  echo 'floating :latest image is forbidden' >&2; exit 1
fi
if grep -Eq '^[[:space:]]*-[[:space:]]+0\.0\.0\.0:|^[[:space:]]*-[[:space:]]+"0\.0\.0\.0:' docker-compose.yml; then
  echo 'Community host ports must not bind to 0.0.0.0' >&2; exit 1
fi
grep -q 'USER 10001:10001' Dockerfile || { echo 'application image must run non-root' >&2; exit 1; }
grep -q 'no-new-privileges:true' docker-compose.yml || { echo 'no-new-privileges is required' >&2; exit 1; }
grep -q 'read_only: true' docker-compose.yml || { echo 'read-only application filesystems are required' >&2; exit 1; }
python3 - <<'PY'
import hashlib,json,pathlib,sys
root=pathlib.Path('.')
cat=json.loads((root/'migrations/catalog.json').read_text())['migrations']
lines=(root/'deploy/postgres/catalog.tsv').read_text().splitlines()
if not lines or lines[0] != 'version\tname\tfile\tphase\trisk\tsha256\thistory_mode':
    raise SystemExit('invalid deployment migration catalog header')
want=[]
for m in cat:
    path=root/'migrations'/m['file']
    digest=hashlib.sha256(path.read_bytes()).hexdigest()
    if digest != m['sha256']:
        raise SystemExit(f"canonical migration checksum drift: {m['file']}")
    want.append('\t'.join(map(str,[m['version'],m['name'],m['file'],m['phase'],m['risk'],m['sha256'],m['history_mode']])))
if lines[1:] != want:
    raise SystemExit('deploy/postgres/catalog.tsv drifted from migrations/catalog.json')
legacy=json.loads((root/'migrations_legacy_pre_v1/catalog.json').read_text())['migrations']
legacy_lines=(root/'deploy/postgres/legacy_pre_v1_catalog.tsv').read_text().splitlines()
legacy_want=['\t'.join(map(str,[m['version'],m['name'],m['file'],m['phase'],m['risk'],m['sha256'],m['history_mode']])) for m in legacy]
if legacy_lines[0] != lines[0] or legacy_lines[1:] != legacy_want:
    raise SystemExit('deploy/postgres/legacy_pre_v1_catalog.tsv drifted from archived pre-v1 catalog')
legacy_digest=hashlib.sha256((root/'migrations_legacy_pre_v1/catalog.json').read_bytes()).hexdigest()
sha_line=(root/'deploy/postgres/legacy_pre_v1_catalog.sha256').read_text().split()[0]
if sha_line != legacy_digest:
    raise SystemExit('legacy pre-v1 catalog SHA-256 metadata drift')
manifest=json.loads((root/'migrations/baseline-manifest.json').read_text())
if len(cat) < 11 or manifest.get('baseline_migration_count') != 11 or manifest.get('legacy_head_version') != 74 or manifest.get('legacy_catalog_sha256') != legacy_digest:
    raise SystemExit('pre-v1 baseline manifest invariant failed')
realm=json.loads((root/'deploy/keycloak/torgnexa-realm.json').read_text())
if realm.get('realm')!='torgnexa' or realm.get('enabled') is not True:
    raise SystemExit('invalid Keycloak realm')
roles={r['name'] for r in realm['roles']['realm']}
if roles != {'admin','manager','operator','viewer'}:
    raise SystemExit('Keycloak role baseline drift')

import re
compose=(root/'docker-compose.yml').read_text()
for match in re.finditer(r'(?m)^\s+image:\s+([^#\s]+)', compose):
    ref=match.group(1)
    if not re.search(r'@sha256:[0-9a-f]{64}$', ref):
        raise SystemExit(f'external Compose image is not digest pinned: {ref}')
dockerfile=(root/'Dockerfile').read_text().splitlines()
for line in dockerfile:
    line=line.strip()
    if not line.startswith('FROM ') or line.startswith('FROM scratch'):
        continue
    ref=line.split()[1]
    if not re.search(r'@sha256:[0-9a-f]{64}$', ref):
        raise SystemExit(f'external Dockerfile image is not digest pinned: {ref}')
PY
# Validate Compose with the real parser when Docker Compose is available.
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  tmp="$(mktemp)"; trap 'rm -f "$tmp"' EXIT
  cat > "$tmp" <<'EOF'
POSTGRES_PASSWORD=test-postgres-secret
TORGNEXA_APP_DB_PASSWORD=test-app-role-secret
TORGNEXA_SECRETS_MASTER_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
KEYCLOAK_DB_PASSWORD=test-keycloak-db-secret
KEYCLOAK_ADMIN_PASSWORD=test-keycloak-admin-secret
GARAGE_RPC_SECRET=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
GARAGE_ADMIN_TOKEN=abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789
GARAGE_METRICS_TOKEN=fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210
S3_ACCESS_KEY=GK0123456789abcdef0123456789abcdef
S3_SECRET_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
CLICKHOUSE_USERNAME=torgnexa
CLICKHOUSE_PASSWORD=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
EOF
  docker compose --env-file "$tmp" config --quiet
fi
echo 'community deployment policy: PASS'
