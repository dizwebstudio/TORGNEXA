#!/usr/bin/env bash
set -euo pipefail
umask 077
export GOTOOLCHAIN=local GOTELEMETRY=off LC_ALL=C TZ=UTC
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"
for required in docker python3 jq go; do command -v "$required" >/dev/null; done
scratch="$(mktemp -d /tmp/torgnexa-audit-pg.XXXXXXXX)"
container_name="torgnexa-audit-regression-${BASHPID}"
started=false
cleanup() {
  if [[ "$started" == true ]]; then docker stop --time 5 "$container_name" >/dev/null; fi
  rm -rf -- "$scratch"
}
trap cleanup EXIT
mkdir "$scratch/socket"
# PostgreSQL's container user owns the actual Unix socket; the directory is disposable.
chmod 0777 "$scratch/socket"
postgres_image="${TORGNEXA_TEST_POSTGRES_IMAGE:-$(jq -er '.development_runtime[] | select(.name == "postgres") | .image' supply-chain/release-artifacts.json)}"
docker run --rm --detach --name "$container_name" --network none --memory 768m --cpus 2 --pids-limit 256 \
  --tmpfs /var/lib/postgresql --mount "type=bind,source=$scratch/socket,target=/var/run/postgresql" \
  --env POSTGRES_HOST_AUTH_METHOD=trust --env POSTGRES_DB=audit "$postgres_image" >/dev/null
started=true
ready=false
for _ in {1..30}; do
  if docker exec "$container_name" pg_isready -U postgres -d audit >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
[[ "$ready" == true ]] || { echo 'Temporary PostgreSQL unavailable' >&2; exit 1; }
python3 - <<'PY' > "$scratch/schema.sql"
import hashlib
import json
import re
from pathlib import Path

root = Path('migrations')
entries = json.loads((root / 'catalog.json').read_text())['migrations']
seeded = False
for entry in entries:
    filename = entry['file']
    if not re.fullmatch(r'[0-9]{6}_[a-z][a-z0-9_]+\.sql', filename):
        raise SystemExit('Unsafe migration filename')
    raw = (root / filename).read_bytes()
    if hashlib.sha256(raw).hexdigest() != entry['sha256']:
        raise SystemExit('Migration checksum mismatch')
    if entry['history_mode'] != 'bootstrap':
        if not seeded:
            rows = []
            for item in entries:
                if item['history_mode'] != 'bootstrap':
                    continue
                values = [str(item['version']), item['name'], item['file'], item['phase'], item['risk'], item['sha256'], '0.1.0', '018f0000-0000-7000-8000-000000000234', '0']
                rows.append('(' + ','.join("'" + value.replace("'", "''") + "'" for value in values) + ')')
            print('INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES ' + ','.join(rows) + ';')
            seeded = True
        settings = {'migration_version': entry['version'], 'migration_name': entry['name'], 'migration_file': filename, 'migration_phase': entry['phase'], 'migration_risk': entry['risk'], 'migration_checksum': entry['sha256'], 'application_version': '0.1.0', 'migration_execution_id': '018f0000-0000-7000-8000-000000000234', 'migration_duration_ms': 0}
        for key, value in settings.items():
            print("SET torgnexa." + key + " = '" + str(value).replace("'", "''") + "';")
    print(raw.decode())
print('CREATE ROLE audit_integration LOGIN NOSUPERUSER NOBYPASSRLS;')
print('GRANT SELECT,INSERT,UPDATE ON ALL TABLES IN SCHEMA public TO audit_integration;')
PY
if ! docker exec -i "$container_name" psql -X -v ON_ERROR_STOP=1 -U postgres -d audit < "$scratch/schema.sql" > "$scratch/migrations.log" 2>&1; then
  tail -30 "$scratch/migrations.log" >&2
  exit 1
fi
export TORGNEXA_TEST_DATABASE_URL="host=$scratch/socket user=audit_integration dbname=audit sslmode=disable"
export TORGNEXA_TEST_ADMIN_DATABASE_URL="host=$scratch/socket user=postgres dbname=audit sslmode=disable"
go test -count=1 -race -v ./internal/app/api ./internal/app/worker ./internal/platform/connectorauth -run 'TestA0[2-9]Postgres'
