#!/bin/sh
set -eu
umask 077

: "${PGHOST:?PGHOST is required}" "${PGDATABASE:?PGDATABASE is required}" "${PGUSER:?PGUSER is required}" "${PGPASSWORD:?PGPASSWORD is required}"
: "${ACTIVE_MIGRATION_CATALOG:=/deploy/postgres/catalog.tsv}"
: "${LEGACY_MIGRATION_CATALOG:=/deploy/postgres/legacy_pre_v1_catalog.tsv}"
: "${LEGACY_CATALOG_SHA_FILE:=/deploy/postgres/legacy_pre_v1_catalog.sha256}"
: "${TORGNEXA_VERSION:=0.1.0-dev}"

[ "${TORGNEXA_ALLOW_PRE_V1_REBASELINE:-}" = "I_UNDERSTAND_THIS_REWRITES_MIGRATION_HISTORY" ] || {
  echo 'pre-v1 rebaseline is disabled; set TORGNEXA_ALLOW_PRE_V1_REBASELINE=I_UNDERSTAND_THIS_REWRITES_MIGRATION_HISTORY' >&2
  exit 1
}
echo "$TORGNEXA_VERSION" | grep -Eq '^[0-9]+[.][0-9]+[.][0-9]+(-[0-9A-Za-z.-]+)?([+][0-9A-Za-z.-]+)?$' || {
  echo 'TORGNEXA_VERSION must be SemVer' >&2
  exit 1
}
[ -f "$ACTIVE_MIGRATION_CATALOG" ] && [ -f "$LEGACY_MIGRATION_CATALOG" ] && [ -f "$LEGACY_CATALOG_SHA_FILE" ] || {
  echo 'pre-v1 rebaseline metadata is missing' >&2
  exit 1
}

export PGPASSWORD
psql_base="psql --no-psqlrc --set ON_ERROR_STOP=1 --host=$PGHOST --port=${PGPORT:-5432} --username=$PGUSER --dbname=$PGDATABASE"
q() { $psql_base --tuples-only --no-align --quiet --command "$1" | tr -d '[:space:]'; }

valid_meta() {
  echo "$1" | grep -Eq '^[0-9]+$' && echo "$2" | grep -Eq '^[a-z][a-z0-9_]{1,62}$' && \
  echo "$3" | grep -Eq '^[0-9]{6}_[a-z][a-z0-9_]{1,62}[.]sql$' && echo "$4" | grep -Eq '^(expand|migrate|contract)$' && \
  echo "$5" | grep -Eq '^(low|medium|high|critical)$' && echo "$6" | grep -Eq '^[0-9a-f]{64}$' && \
  echo "$7" | grep -Eq '^(bootstrap|atomic)$'
}

history_exists=$(q "SELECT CASE WHEN to_regclass('public.migration_history') IS NULL THEN 0 ELSE 1 END")
[ "$history_exists" = 1 ] || { echo 'migration_history is missing; fresh databases must use the compact baseline directly' >&2; exit 1; }
legacy_expected=$(($(wc -l < "$LEGACY_MIGRATION_CATALOG")-1))
active_expected=$(($(wc -l < "$ACTIVE_MIGRATION_CATALOG")-1))
[ "$legacy_expected" -eq 74 ] || { echo "legacy catalog must contain 74 migrations, got $legacy_expected" >&2; exit 1; }
[ "$active_expected" -eq 11 ] || { echo "active pre-v1 baseline must contain 11 migrations, got $active_expected" >&2; exit 1; }
applied=$(q 'SELECT count(*) FROM migration_history')
[ "$applied" -eq "$legacy_expected" ] || {
  echo "database has $applied migration_history rows; exact legacy pre-v1 head requires $legacy_expected" >&2
  exit 1
}

first=true
while IFS="$(printf '\t')" read -r version name file phase risk checksum history; do
  if [ "$first" = true ]; then first=false; continue; fi
  valid_meta "$version" "$name" "$file" "$phase" "$risk" "$checksum" "$history" || { echo "invalid legacy metadata for $file" >&2; exit 1; }
  row=$($psql_base --tuples-only --no-align --quiet --field-separator='|' --command "SELECT name,checksum_sha256 FROM migration_history WHERE version=$version")
  [ "$row" = "$name|$checksum" ] || {
    echo "legacy migration history mismatch at version $version; refusing to stamp baseline" >&2
    exit 1
  }
done < "$LEGACY_MIGRATION_CATALOG"

for relation in organizations workspaces inbox_receipts fulfillment_allocations warehouse_incidents connector_runtime_configs; do
  exists=$(q "SELECT CASE WHEN to_regclass('public.$relation') IS NULL THEN 0 ELSE 1 END")
  [ "$exists" = 1 ] || { echo "legacy head sentinel relation is missing: $relation" >&2; exit 1; }
done

legacy_catalog_sha=$(awk 'NR==1 {print $1}' "$LEGACY_CATALOG_SHA_FILE")
echo "$legacy_catalog_sha" | grep -Eq '^[0-9a-f]{64}$' || { echo 'invalid legacy catalog SHA-256 metadata' >&2; exit 1; }

{
  echo 'BEGIN;'
  echo 'LOCK TABLE migration_history IN ACCESS EXCLUSIVE MODE;'
  echo 'CREATE TABLE IF NOT EXISTS migration_history_legacy_pre_v1 AS TABLE migration_history WITH NO DATA;'
  echo 'REVOKE ALL ON migration_history_legacy_pre_v1 FROM PUBLIC;'
  echo 'INSERT INTO migration_history_legacy_pre_v1 SELECT h.* FROM migration_history h WHERE NOT EXISTS (SELECT 1 FROM migration_history_legacy_pre_v1 a WHERE a.version=h.version);'
  cat <<'SQL'
CREATE TABLE IF NOT EXISTS migration_baseline_evidence (
  baseline_id text PRIMARY KEY,
  source_head_version integer NOT NULL CHECK (source_head_version > 0),
  source_catalog_sha256 text NOT NULL CHECK (source_catalog_sha256 ~ '^[0-9a-f]{64}$'),
  source_history_rows integer NOT NULL CHECK (source_history_rows >= 0),
  mode text NOT NULL CHECK (mode IN ('fresh_baseline','legacy_rebaseline')),
  stamped_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
REVOKE ALL ON migration_baseline_evidence FROM PUBLIC;
DELETE FROM migration_history;
SQL
  first=true
  while IFS="$(printf '\t')" read -r version name file phase risk checksum history; do
    if [ "$first" = true ]; then first=false; continue; fi
    valid_meta "$version" "$name" "$file" "$phase" "$risk" "$checksum" "$history" || exit 1
    printf "INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms) VALUES (%s,'%s','%s','%s','%s','%s','%s','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670',0);\n" \
      "$version" "$name" "$file" "$phase" "$risk" "$checksum" "$TORGNEXA_VERSION"
  done < "$ACTIVE_MIGRATION_CATALOG"
  printf "INSERT INTO migration_baseline_evidence(baseline_id,source_head_version,source_catalog_sha256,source_history_rows,mode) VALUES ('pre_v1_v1',74,'%s',74,'legacy_rebaseline') ON CONFLICT (baseline_id) DO UPDATE SET source_head_version=EXCLUDED.source_head_version,source_catalog_sha256=EXCLUDED.source_catalog_sha256,source_history_rows=EXCLUDED.source_history_rows,mode=EXCLUDED.mode,stamped_at=clock_timestamp();\n" "$legacy_catalog_sha"
  echo 'COMMIT;'
} | $psql_base --file - >/dev/null

active_rows=$(q 'SELECT count(*) FROM migration_history')
archive_rows=$(q 'SELECT count(*) FROM migration_history_legacy_pre_v1')
[ "$active_rows" -eq "$active_expected" ] && [ "$archive_rows" -eq "$legacy_expected" ] || {
  echo "rebaseline verification failed: active=$active_rows archive=$archive_rows" >&2
  exit 1
}

echo "TORGNEXA pre-v1 rebaseline complete: legacy $archive_rows rows archived; active baseline $active_rows rows stamped"
