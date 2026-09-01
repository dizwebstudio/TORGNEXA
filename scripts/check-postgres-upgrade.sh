#!/usr/bin/env bash
set -euo pipefail

umask 077
export LC_ALL=C
export TZ=UTC

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
inventory="$repo_root/supply-chain/release-artifacts.json"
catalog="$repo_root/migrations/catalog.json"
container_name="torgnexa-postgres-upgrade-${BASHPID}"
container_started=false

die() {
  echo "check-postgres-upgrade: $*" >&2
  exit 1
}

for command_name in docker jq mktemp rm sha256sum tail tr; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done

postgres_image="$(jq -er '[.development_runtime[] | select(.name == "postgres") | .image] | if length == 1 then .[0] else error("expected exactly one postgres image") end' "$inventory")" || \
  die "PostgreSQL runtime image is not registered exactly once"
[[ "$postgres_image" =~ ^postgres:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$ ]] || die "PostgreSQL image is not immutable"
[[ "$container_name" =~ ^torgnexa-postgres-upgrade-[0-9]+$ ]] || die "unsafe container name"
[[ -z "$(docker ps --all --filter "name=^/${container_name}$" --format '{{.Names}}')" ]] || die "temporary container name already exists"

cleanup() {
  if [[ "$container_started" == true ]]; then
    if ! docker stop --time 5 "$container_name" >/dev/null; then
      echo "check-postgres-upgrade: unable to stop temporary container $container_name" >&2
    fi
  fi
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

docker run --rm --detach \
  --name "$container_name" \
  --network none \
  --read-only \
  --tmpfs /tmp:rw,nosuid,noexec,size=768m,mode=1777 \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --memory 1g \
  --cpus 1 \
  --pids-limit 256 \
  --user postgres \
  --entrypoint sleep \
  "$postgres_image" infinity >/dev/null
container_started=true

docker_exec() {
  docker exec --user postgres "$container_name" "$@"
}

if ! docker_exec sh -eu -c '
  mkdir -m 0700 /tmp/cluster
  initdb --pgdata=/tmp/cluster --encoding=UTF8 --no-locale --auth-local=trust --auth-host=reject >/tmp/initdb.log 2>&1
'; then
  if ! docker_exec tail -n 100 /tmp/initdb.log >&2; then
    echo "check-postgres-upgrade: initdb log is unavailable" >&2
  fi
  die "unable to initialize the synthetic upgrade cluster"
fi
docker_exec sh -eu -c 'printf "%s\n" "$@" >>/tmp/cluster/postgresql.conf' sh \
  "listen_addresses = ''" \
  "unix_socket_directories = '/tmp'" \
  "unix_socket_permissions = 0700" \
  "port = 5432" \
  "fsync = on" \
  "synchronous_commit = on" \
  "full_page_writes = on"
if ! docker_exec pg_ctl --pgdata=/tmp/cluster --wait --timeout=30 --log=/tmp/postgres.log start; then
  if ! docker_exec tail -n 100 /tmp/postgres.log >&2; then
    echo "check-postgres-upgrade: PostgreSQL log is unavailable" >&2
  fi
  die "unable to start the synthetic upgrade cluster"
fi

psql_exec() {
  local database=$1
  shift
  docker exec --interactive --user postgres "$container_name" \
    psql --no-psqlrc --set ON_ERROR_STOP=1 --host /tmp --port 5432 \
    --username postgres --dbname "$database" "$@"
}

query_scalar() {
  local database=$1
  local statement=$2
  psql_exec "$database" --tuples-only --no-align --quiet --command "$statement" |
    tail -n 1 | tr -d '[:space:]'
}

apply_migration() {
  local database=$1
  local file=$2
  local expected=$3
  local path="$repo_root/migrations/$file"
  local digest_line actual
  [[ "$file" =~ ^[0-9]{6}_[a-z][a-z0-9_]{1,62}\.sql$ && "$expected" =~ ^[0-9a-f]{64}$ ]] || die "unsafe migration metadata"
  [[ -f "$path" && ! -L "$path" ]] || die "unsafe migration path: $file"
  digest_line="$(sha256sum -- "$path")"
  actual="${digest_line%% *}"
  [[ "$actual" == "$expected" ]] || die "migration checksum drift: $file"
  psql_exec "$database" --file - <"$path" >/dev/null
}

apply_atomic_migration() {
  local database=$1 version=$2 name=$3 file=$4 phase=$5 risk=$6 checksum=$7
  local path="$repo_root/migrations/$file"
  local migration_input
  [[ -f "$path" && ! -L "$path" ]] || die "unsafe migration path: $file"
  {
    printf "SET torgnexa.migration_version = '%s';\n" "$version"
    printf "SET torgnexa.migration_name = '%s';\n" "$name"
    printf "SET torgnexa.migration_file = '%s';\n" "$file"
    printf "SET torgnexa.migration_phase = '%s';\n" "$phase"
    printf "SET torgnexa.migration_risk = '%s';\n" "$risk"
    printf "SET torgnexa.migration_checksum = '%s';\n" "$checksum"
    printf "SET torgnexa.application_version = '0.1.0';\n"
    printf "SET torgnexa.migration_execution_id = '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670';\n"
    printf "SET torgnexa.migration_duration_ms = '0';\n"
    cat -- "$path"
  } >"${migration_input:=$(mktemp)}"
  if ! psql_exec "$database" --file - <"$migration_input" >/dev/null; then
    rm -f -- "$migration_input"
    return 1
  fi
  rm -f -- "$migration_input"
}

catalog_rows="$(jq -er '.migrations[] | [.version, .name, .file, .phase, .risk, .sha256, .history_mode] | @tsv' "$catalog")" || die "unable to read migration catalog"

psql_exec postgres --command "CREATE DATABASE torgnexa_upgrade;" >/dev/null
psql_exec postgres --command "CREATE DATABASE torgnexa_fresh;" >/dev/null
while IFS=$'\t' read -r version _ file _ _ checksum _; do
  if ((version <= 2)); then
    apply_migration torgnexa_upgrade "$file" "$checksum"
  fi
done < <(printf '%s\n' "$catalog_rows")

psql_exec torgnexa_upgrade --command "
  INSERT INTO organizations (id, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', 'Synthetic Organization A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', 'Synthetic Organization B');
  INSERT INTO workspaces (id, organization_id, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', 'Synthetic Workspace A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', 'Synthetic Workspace B');
  INSERT INTO stores (id, organization_id, workspace_id, code, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'synthetic-a', 'Synthetic Store A');
" >/dev/null

old_shape_before="$(query_scalar torgnexa_upgrade "SELECT count(*) FROM organizations o JOIN workspaces w ON w.organization_id=o.id JOIN stores s ON s.organization_id=w.organization_id AND s.workspace_id=w.id;")"
[[ "$old_shape_before" == 1 ]] || die "old application read failed before framework expansion"

framework_row="$(printf '%s\n' "$catalog_rows" | awk -F '\t' '$1 == 3 {print; exit}')"
IFS=$'\t' read -r framework_version _ framework_file _ _ framework_checksum _ <<<"$framework_row"
[[ "$framework_version" == 3 ]] || die "expected migration framework at version 3"
apply_migration torgnexa_upgrade "$framework_file" "$framework_checksum"

old_shape_after="$(query_scalar torgnexa_upgrade "SELECT count(*) FROM organizations o JOIN workspaces w ON w.organization_id=o.id JOIN stores s ON s.organization_id=w.organization_id AND s.workspace_id=w.id;")"
[[ "$old_shape_after" == "$old_shape_before" ]] || die "expand migration broke the old application read shape"
psql_exec torgnexa_upgrade --command "
  INSERT INTO stores (id, organization_id, workspace_id, code, name)
  VALUES ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0004',
          '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',
          '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',
          'old-writer', 'Synthetic Old Writer Store');
" >/dev/null
old_write_after="$(query_scalar torgnexa_upgrade "SELECT count(*) FROM stores WHERE workspace_id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002';")"
[[ "$old_write_after" == 2 ]] || die "expand migration broke the old application write shape"

history_values=
while IFS=$'\t' read -r version name file phase risk checksum history_mode; do
  [[ "$history_mode" == bootstrap ]] || continue
  value="($version, '$name', '$file', '$phase', '$risk', '$checksum', '0.1.0', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670', 0)"
  if [[ -z "$history_values" ]]; then
    history_values=$value
  else
    history_values="$history_values, $value"
  fi
done < <(printf '%s\n' "$catalog_rows")
psql_exec torgnexa_upgrade --command "
  INSERT INTO migration_history (
    version, name, file_name, phase, risk, checksum_sha256,
    application_version, execution_id, duration_ms
  ) VALUES $history_values;
" >/dev/null

while IFS=$'\t' read -r version name file phase risk checksum history_mode; do
  [[ "$history_mode" == atomic ]] || continue
  apply_atomic_migration torgnexa_upgrade "$version" "$name" "$file" "$phase" "$risk" "$checksum"
done < <(printf '%s\n' "$catalog_rows")

history_count="$(query_scalar torgnexa_upgrade "SELECT count(*) FROM migration_history;")"
catalog_count="$(jq -er '.migrations | length' "$catalog")"
[[ "$history_count" == "$catalog_count" ]] || die "migration history is incomplete"
while IFS=$'\t' read -r version name _ _ _ checksum _; do
  match="$(query_scalar torgnexa_upgrade "SELECT count(*) FROM migration_history WHERE version=$version AND name='$name' AND checksum_sha256='$checksum';")"
  [[ "$match" == 1 ]] || die "migration history drift at version $version"
done < <(printf '%s\n' "$catalog_rows")

psql_exec torgnexa_upgrade --command "
  CREATE ROLE torgnexa_upgrade_app NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
  GRANT SELECT ON organizations, workspaces, stores TO torgnexa_upgrade_app;
" >/dev/null
if psql_exec torgnexa_upgrade --command "SET ROLE torgnexa_upgrade_app; SELECT count(*) FROM migration_history;" >/dev/null 2>&1; then
  die "application role unexpectedly read privileged migration history"
fi
if psql_exec torgnexa_upgrade --command "SET ROLE torgnexa_upgrade_app; UPDATE backfill_jobs SET state='completed';" >/dev/null 2>&1; then
  die "application role unexpectedly mutated backfill checkpoints"
fi

psql_exec torgnexa_upgrade --command "
  CREATE TABLE rehearsal_source (id integer PRIMARY KEY, value text NOT NULL);
  CREATE TABLE rehearsal_target (id integer PRIMARY KEY, value text NOT NULL);
  INSERT INTO rehearsal_source (id, value)
    SELECT value, 'synthetic-' || value::text FROM generate_series(1, 5) AS value;
  INSERT INTO backfill_jobs (id, job_key, batch_size)
  VALUES ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671', 'synthetic.upgrade_backfill', 2);
" >/dev/null

generation_one="$(query_scalar torgnexa_upgrade "
  UPDATE backfill_jobs
  SET state='running', lease_owner='worker-a', lease_until=clock_timestamp()+interval '5 minutes',
      lease_generation=lease_generation+1, attempts=attempts+1, version=version+1, updated_at=clock_timestamp()
  WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671' AND state='pending'
  RETURNING lease_generation;
")"
[[ "$generation_one" == 1 ]] || die "first backfill lease was not acquired"
psql_exec torgnexa_upgrade --command "
  BEGIN;
  INSERT INTO rehearsal_target SELECT * FROM rehearsal_source WHERE id <= 2
    ON CONFLICT (id) DO UPDATE SET value=EXCLUDED.value;
  UPDATE backfill_jobs
  SET checkpoint='2', processed_count=processed_count+2, state='pending',
      lease_owner=NULL, lease_until=NULL, last_error_code=NULL,
      version=version+1, updated_at=clock_timestamp()
  WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671'
    AND lease_owner='worker-a' AND lease_generation=1 AND state='running';
  COMMIT;
" >/dev/null

generation_two="$(query_scalar torgnexa_upgrade "
  UPDATE backfill_jobs
  SET state='running', lease_owner='worker-a', lease_until=clock_timestamp()+interval '5 minutes',
      lease_generation=lease_generation+1, attempts=attempts+1, version=version+1, updated_at=clock_timestamp()
  WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671' AND state='pending'
  RETURNING lease_generation;
")"
[[ "$generation_two" == 2 ]] || die "second backfill lease was not acquired"
psql_exec torgnexa_upgrade --command "
  BEGIN;
  INSERT INTO rehearsal_target SELECT * FROM rehearsal_source WHERE id > 2 AND id <= 4
    ON CONFLICT (id) DO UPDATE SET value=EXCLUDED.value;
  UPDATE backfill_jobs
  SET checkpoint='4', processed_count=processed_count+2, state='pending',
      lease_owner=NULL, lease_until=NULL, version=version+1, updated_at=clock_timestamp()
  WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671'
    AND lease_owner='worker-a' AND lease_generation=2 AND state='running';
  ROLLBACK;
" >/dev/null

interrupted_state="$(query_scalar torgnexa_upgrade "SELECT checkpoint || ':' || processed_count || ':' || state || ':' || (SELECT count(*) FROM rehearsal_target) FROM backfill_jobs WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671';")"
[[ "$interrupted_state" == "2:2:running:2" ]] || die "interrupted batch was not atomically rolled back: $interrupted_state"
psql_exec torgnexa_upgrade --command "UPDATE backfill_jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671';" >/dev/null
stale_commits="$(query_scalar torgnexa_upgrade "
  WITH stale AS (
    UPDATE backfill_jobs SET checkpoint='unsafe-stale'
    WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671'
      AND lease_owner='worker-a' AND lease_generation=1 AND state='running'
    RETURNING 1
  ) SELECT count(*) FROM stale;
")"
[[ "$stale_commits" == 0 ]] || die "stale lease bypassed the fencing generation"

generation_three="$(query_scalar torgnexa_upgrade "
  UPDATE backfill_jobs
  SET lease_owner='worker-b', lease_until=clock_timestamp()+interval '5 minutes',
      lease_generation=lease_generation+1, attempts=attempts+1, version=version+1, updated_at=clock_timestamp()
  WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671'
    AND state='running' AND lease_until <= clock_timestamp()
  RETURNING lease_generation;
")"
[[ "$generation_three" == 3 ]] || die "expired lease was not safely reclaimed"
psql_exec torgnexa_upgrade --command "
  BEGIN;
  INSERT INTO rehearsal_target SELECT * FROM rehearsal_source WHERE id > 2
    ON CONFLICT (id) DO UPDATE SET value=EXCLUDED.value;
  UPDATE backfill_jobs
  SET checkpoint='5', processed_count=processed_count+3, state='completed',
      lease_owner=NULL, lease_until=NULL, last_error_code=NULL,
      completed_at=clock_timestamp(), version=version+1, updated_at=clock_timestamp()
  WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671'
    AND lease_owner='worker-b' AND lease_generation=3 AND state='running';
  COMMIT;
" >/dev/null

completed_state="$(query_scalar torgnexa_upgrade "SELECT checkpoint || ':' || processed_count || ':' || attempts || ':' || state || ':' || (SELECT count(*) FROM rehearsal_target) FROM backfill_jobs WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671';")"
[[ "$completed_state" == "5:5:3:completed:5" ]] || die "resumed backfill did not complete exactly once: $completed_state"
completed_reclaims="$(query_scalar torgnexa_upgrade "
  WITH claimed AS (
    UPDATE backfill_jobs SET state='running', lease_owner='worker-c', lease_until=clock_timestamp()+interval '1 minute'
    WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671' AND state IN ('pending','failed')
    RETURNING 1
  ) SELECT count(*) FROM claimed;
")"
[[ "$completed_reclaims" == 0 ]] || die "completed backfill was claimed again"

if psql_exec torgnexa_upgrade --command "INSERT INTO backfill_jobs (id, job_key, batch_size) VALUES ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0672', 'synthetic.upgrade_backfill', 2);" >/dev/null 2>&1; then
  die "duplicate global backfill job bypassed NULLS NOT DISTINCT uniqueness"
fi
if psql_exec torgnexa_upgrade --command "INSERT INTO backfill_jobs (id, job_key, organization_id, batch_size) VALUES ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0673', 'synthetic.invalid_scope', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', 2);" >/dev/null 2>&1; then
  die "partial tenant scope bypassed the backfill constraint"
fi

fresh_bootstrap_values=
mapfile -t catalog_lines <<<"$catalog_rows"
for catalog_line in "${catalog_lines[@]}"; do
  IFS=$'\t' read -r version name file phase risk checksum history_mode <<<"$catalog_line"
  if [[ "$history_mode" == bootstrap ]]; then
    apply_migration torgnexa_fresh "$file" "$checksum"
    value="($version, '$name', '$file', '$phase', '$risk', '$checksum', '0.1.0', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670', 0)"
    if [[ -z "$fresh_bootstrap_values" ]]; then
      fresh_bootstrap_values=$value
    else
      fresh_bootstrap_values="$fresh_bootstrap_values, $value"
    fi
    continue
  fi
  if [[ -n "$fresh_bootstrap_values" ]]; then
    psql_exec torgnexa_fresh --command "
      INSERT INTO migration_history (
        version, name, file_name, phase, risk, checksum_sha256,
        application_version, execution_id, duration_ms
      ) VALUES $fresh_bootstrap_values;
    " >/dev/null
    fresh_bootstrap_values=
  fi
  apply_atomic_migration torgnexa_fresh "$version" "$name" "$file" "$phase" "$risk" "$checksum"
done

upgrade_columns="$(query_scalar torgnexa_upgrade "
  SELECT md5(string_agg(table_name || ':' || column_name || ':' || data_type || ':' || is_nullable || ':' || coalesce(column_default,''), ',' ORDER BY table_name, ordinal_position))
  FROM information_schema.columns
  WHERE table_schema='public' AND table_name NOT LIKE 'rehearsal_%';
")"
fresh_columns="$(query_scalar torgnexa_fresh "
  SELECT md5(string_agg(table_name || ':' || column_name || ':' || data_type || ':' || is_nullable || ':' || coalesce(column_default,''), ',' ORDER BY table_name, ordinal_position))
  FROM information_schema.columns
  WHERE table_schema='public';
")"
[[ "$upgrade_columns" == "$fresh_columns" ]] || die "upgraded and fresh schemas differ"

upgrade_constraints="$(query_scalar torgnexa_upgrade "
  SELECT md5(string_agg(c.relname || ':' || con.conname || ':' || pg_get_constraintdef(con.oid, true), ',' ORDER BY c.relname, con.conname))
  FROM pg_constraint con
  JOIN pg_class c ON c.oid=con.conrelid
  JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname='public' AND c.relname NOT LIKE 'rehearsal_%';
")"
fresh_constraints="$(query_scalar torgnexa_fresh "
  SELECT md5(string_agg(c.relname || ':' || con.conname || ':' || pg_get_constraintdef(con.oid, true), ',' ORDER BY c.relname, con.conname))
  FROM pg_constraint con
  JOIN pg_class c ON c.oid=con.conrelid
  JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname='public';
")"
[[ "$upgrade_constraints" == "$fresh_constraints" ]] || die "upgraded and fresh constraints differ"

constraint_count="$(query_scalar torgnexa_upgrade "SELECT count(*) FROM pg_constraint WHERE connamespace='public'::regnamespace AND NOT convalidated;")"
[[ "$constraint_count" == 0 ]] || die "$constraint_count constraints remain unvalidated"
history_after="$(query_scalar torgnexa_upgrade "SELECT count(*) FROM migration_history;")"
[[ "$history_after" == "$catalog_count" ]] || die "rehearsal rerun changed immutable migration history"

docker_exec pg_ctl --pgdata=/tmp/cluster --wait --timeout=30 --mode fast stop >/dev/null
echo "PostgreSQL upgrade, fresh-install parity, and interrupted/resumed backfill rehearsal passed"
