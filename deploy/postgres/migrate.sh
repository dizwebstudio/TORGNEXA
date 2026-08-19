#!/bin/sh
set -eu
umask 077
: "${PGHOST:?PGHOST is required}" "${PGDATABASE:?PGDATABASE is required}" "${PGUSER:?PGUSER is required}" "${PGPASSWORD:?PGPASSWORD is required}"
: "${MIGRATIONS_DIR:=/migrations}" "${MIGRATION_CATALOG:=/deploy/postgres/catalog.tsv}" "${TORGNEXA_VERSION:=0.1.0-dev}"
echo "$TORGNEXA_VERSION" | grep -Eq '^[0-9]+[.][0-9]+[.][0-9]+(-[0-9A-Za-z.-]+)?([+][0-9A-Za-z.-]+)?$' || { echo 'TORGNEXA_VERSION must be SemVer' >&2; exit 1; }
export PGPASSWORD
psql_base="psql --no-psqlrc --set ON_ERROR_STOP=1 --host=$PGHOST --port=${PGPORT:-5432} --username=$PGUSER --dbname=$PGDATABASE"
run_psql() { sh -c "$psql_base \"$@\""; }
q() { $psql_base --tuples-only --no-align --quiet --command "$1" | tr -d '[:space:]'; }
valid_meta() {
  echo "$1" | grep -Eq '^[0-9]+$' && echo "$2" | grep -Eq '^[a-z][a-z0-9_]{1,62}$' && \
  echo "$3" | grep -Eq '^[0-9]{6}_[a-z][a-z0-9_]{1,62}[.]sql$' && echo "$4" | grep -Eq '^(expand|migrate|contract)$' && \
  echo "$5" | grep -Eq '^(low|medium|high|critical)$' && echo "$6" | grep -Eq '^[0-9a-f]{64}$' && \
  echo "$7" | grep -Eq '^(bootstrap|atomic)$'
}
wait_for_db() {
  i=0
  until pg_isready -h "$PGHOST" -p "${PGPORT:-5432}" -U "$PGUSER" -d "$PGDATABASE" >/dev/null 2>&1; do
    i=$((i+1)); [ "$i" -lt 60 ] || { echo 'database did not become ready' >&2; exit 1; }; sleep 2
  done
}
verify_file() {
  file=$1 expected=$2
  [ -f "$MIGRATIONS_DIR/$file" ] && [ ! -L "$MIGRATIONS_DIR/$file" ] || { echo "unsafe migration file: $file" >&2; exit 1; }
  actual=$(sha256sum "$MIGRATIONS_DIR/$file" | awk '{print $1}')
  [ "$actual" = "$expected" ] || { echo "migration checksum drift: $file" >&2; exit 1; }
}
seed_bootstrap() {
  first=true
  while IFS="$(printf '\t')" read -r version name file phase risk checksum history; do
    if [ "$first" = true ]; then first=false; continue; fi
    [ "$history" = bootstrap ] || continue
    $psql_base --set version="$version" --set name="$name" --set file="$file" --set phase="$phase" --set risk="$risk" --set checksum="$checksum" --set app_version="$TORGNEXA_VERSION" <<'SQL'
INSERT INTO migration_history(version,name,file_name,phase,risk,checksum_sha256,application_version,execution_id,duration_ms)
VALUES (:'version'::integer, :'name', :'file', :'phase', :'risk', :'checksum', :'app_version', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670', 0)
ON CONFLICT (version) DO NOTHING;
SQL
  done < "$MIGRATION_CATALOG"
}
wait_for_db
history_exists=$(q "SELECT CASE WHEN to_regclass('public.migration_history') IS NULL THEN 0 ELSE 1 END")
if [ "$history_exists" = 1 ]; then
  applied_total=$(q 'SELECT count(*) FROM migration_history')
  active_expected=$(($(wc -l < "$MIGRATION_CATALOG")-1))
  legacy_catalog=/deploy/postgres/legacy_pre_v1_catalog.tsv
  if [ -f "$legacy_catalog" ]; then
    legacy_expected=$(($(wc -l < "$legacy_catalog")-1))
    if [ "$applied_total" -eq "$legacy_expected" ] && [ "$legacy_expected" -gt "$active_expected" ]; then
      if [ "${TORGNEXA_ALLOW_PRE_V1_REBASELINE:-}" = "I_UNDERSTAND_THIS_REWRITES_MIGRATION_HISTORY" ]; then
        /deploy/postgres/rebaseline-pre-v1.sh
        history_exists=1
      else
        echo 'legacy pre-v1 migration history detected; run the reviewed one-time rebaseline before using the compact baseline' >&2
        echo 'set TORGNEXA_ALLOW_PRE_V1_REBASELINE=I_UNDERSTAND_THIS_REWRITES_MIGRATION_HISTORY and execute /deploy/postgres/rebaseline-pre-v1.sh' >&2
        exit 1
      fi
    fi
  fi
fi
if [ "$history_exists" = 0 ]; then
  bootstrap_objects=$(q "SELECT CASE WHEN to_regclass('public.organizations') IS NULL THEN 0 ELSE 1 END")
  [ "$bootstrap_objects" = 0 ] || { echo 'untracked partial bootstrap schema detected; restore/review before retrying migrations' >&2; exit 1; }
else
  bootstrap_rows=$(q 'SELECT count(*) FROM migration_history WHERE version BETWEEN 1 AND 3')
  if [ "$bootstrap_rows" = 0 ]; then seed_bootstrap; elif [ "$bootstrap_rows" -ne 3 ]; then echo 'partial bootstrap migration history detected' >&2; exit 1; fi
fi
# TSV is generated from the reviewed canonical migrations/catalog.json; runtime still verifies every SQL SHA-256.
first=true
while IFS="$(printf '\t')" read -r version name file phase risk checksum history; do
  if [ "$first" = true ]; then first=false; continue; fi
  valid_meta "$version" "$name" "$file" "$phase" "$risk" "$checksum" "$history" || { echo "invalid migration metadata for $file" >&2; exit 1; }
  verify_file "$file" "$checksum"
  if [ "$history_exists" = 1 ]; then
    applied=$(q "SELECT count(*) FROM migration_history WHERE version=$version")
    if [ "$applied" = 1 ]; then
      stored=$(q "SELECT checksum_sha256 FROM migration_history WHERE version=$version")
      [ "$stored" = "$checksum" ] || { echo "applied migration checksum mismatch: $file" >&2; exit 1; }
      continue
    fi
  fi
  if [ "$history" = bootstrap ]; then
    echo "applying bootstrap migration $file"
    $psql_base --file "$MIGRATIONS_DIR/$file" >/dev/null
    [ "$version" -lt 3 ] || { seed_bootstrap; history_exists=1; }
    continue
  fi
  [ "$history_exists" = 1 ] || { echo 'migration framework/history missing before atomic migration' >&2; exit 1; }
  echo "applying atomic migration $file"
  {
    printf "SET torgnexa.migration_version = '%s';\n" "$version"
    printf "SET torgnexa.migration_name = '%s';\n" "$name"
    printf "SET torgnexa.migration_file = '%s';\n" "$file"
    printf "SET torgnexa.migration_phase = '%s';\n" "$phase"
    printf "SET torgnexa.migration_risk = '%s';\n" "$risk"
    printf "SET torgnexa.migration_checksum = '%s';\n" "$checksum"
    printf "SET torgnexa.application_version = '%s';\n" "$TORGNEXA_VERSION"
    printf "SET torgnexa.migration_execution_id = '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670';\n"
    printf "SET torgnexa.migration_duration_ms = '0';\n"
    cat "$MIGRATIONS_DIR/$file"
  } | $psql_base --file - >/dev/null
done < "$MIGRATION_CATALOG"
expected=$(($(wc -l < "$MIGRATION_CATALOG")-1))
applied=$(q 'SELECT count(*) FROM migration_history')
[ "$applied" -eq "$expected" ] || { echo "migration history has $applied rows; expected $expected" >&2; exit 1; }
echo "TORGNEXA migrations complete: $applied/$expected"
