#!/usr/bin/env bash
set -euo pipefail

umask 077
export LC_ALL=C
export TZ=UTC

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
inventory="$repo_root/supply-chain/release-artifacts.json"
container_name="torgnexa-postgres-restore-${BASHPID}"
evidence_file=
evidence_tmp=
container_started=false

die() {
  echo "check-postgres-backup-restore: $*" >&2
  exit 1
}

usage() {
  echo "usage: $0 [--evidence-file ABSOLUTE_NEW_FILE]" >&2
}

while (($# > 0)); do
  case "$1" in
    --evidence-file)
      (($# >= 2)) || {
        usage
        exit 2
      }
      [[ -z "$evidence_file" ]] || die "--evidence-file may be specified only once"
      evidence_file=$2
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage
      die "unsupported argument: $1"
      ;;
  esac
done

for command_name in date dirname docker jq mktemp mv pwd sha256sum stat tail tr; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done

if [[ -n "$evidence_file" ]]; then
  [[ "$evidence_file" == /* ]] || die "evidence path must be absolute"
  [[ ! -e "$evidence_file" && ! -L "$evidence_file" ]] || die "evidence path must not already exist"
  evidence_parent="$(dirname -- "$evidence_file")"
  [[ -d "$evidence_parent" && ! -L "$evidence_parent" ]] || die "evidence parent must be an existing non-symlink directory"
  evidence_parent="$(cd -- "$evidence_parent" && pwd -P)"
  evidence_file="$evidence_parent/$(basename -- "$evidence_file")"
  if [[ -n "${TORGNEXA_SAFE_OUTPUT_ROOT:-}" ]]; then
    [[ "$TORGNEXA_SAFE_OUTPUT_ROOT" == /* ]] || die "TORGNEXA_SAFE_OUTPUT_ROOT must be absolute"
    [[ -d "$TORGNEXA_SAFE_OUTPUT_ROOT" && ! -L "$TORGNEXA_SAFE_OUTPUT_ROOT" ]] || die "TORGNEXA_SAFE_OUTPUT_ROOT must be an existing non-symlink directory"
    safe_root="$(cd -- "$TORGNEXA_SAFE_OUTPUT_ROOT" && pwd -P)"
    [[ "$evidence_file" == "$safe_root/"* ]] || die "evidence path is outside TORGNEXA_SAFE_OUTPUT_ROOT"
  fi
fi

[[ "$container_name" =~ ^torgnexa-postgres-restore-[0-9]+$ ]] || die "unsafe container name"
postgres_image="$(jq -er '[.development_runtime[] | select(.name == "postgres") | .image] | if length == 1 then .[0] else error("expected exactly one postgres image") end' "$inventory")" || \
  die "PostgreSQL runtime image is not registered exactly once"
[[ "$postgres_image" =~ ^postgres:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$ ]] || \
  die "PostgreSQL runtime image is not immutable"
[[ -z "$(docker ps --all --filter "name=^/${container_name}$" --format '{{.Names}}')" ]] || \
  die "temporary container name already exists"

cleanup() {
  if [[ -n "$evidence_tmp" && ( -e "$evidence_tmp" || -L "$evidence_tmp" ) ]]; then
    rm -- "$evidence_tmp"
  fi
  if [[ "$container_started" == true ]]; then
    if ! docker stop --time 5 "$container_name" >/dev/null; then
      echo "check-postgres-backup-restore: unable to stop temporary container $container_name" >&2
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
  mkdir -m 0700 /tmp/primary /tmp/wal_archive
  initdb --pgdata=/tmp/primary --encoding=UTF8 --no-locale --auth-local=trust --auth-host=reject >/tmp/initdb.log 2>&1
'; then
  if ! docker_exec tail -n 100 /tmp/initdb.log >&2; then
    echo "check-postgres-backup-restore: initdb log is unavailable" >&2
  fi
  die "unable to initialize the synthetic primary"
fi
docker_exec sh -eu -c 'printf "%s\n" "$@" >>/tmp/primary/postgresql.conf' sh \
  "listen_addresses = ''" \
  "unix_socket_directories = '/tmp'" \
  "unix_socket_permissions = 0700" \
  "port = 5432" \
  "wal_level = replica" \
  "max_wal_senders = 2" \
  "archive_mode = on" \
  "archive_timeout = '1s'" \
  "archive_command = 'test ! -f /tmp/wal_archive/%f && cp %p /tmp/wal_archive/%f'" \
  "fsync = on" \
  "synchronous_commit = on" \
  "full_page_writes = on"
if ! docker_exec pg_ctl --pgdata=/tmp/primary --wait --timeout=30 --log=/tmp/primary.log start; then
  if ! docker_exec tail -n 100 /tmp/primary.log >&2; then
    echo "check-postgres-backup-restore: primary log is unavailable" >&2
  fi
  die "unable to start the synthetic primary"
fi

psql_exec() {
  local port=$1
  local database=$2
  shift 2
  docker exec --interactive --user postgres "$container_name" \
    psql --no-psqlrc --set ON_ERROR_STOP=1 --host /tmp --port "$port" \
    --username postgres --dbname "$database" "$@"
}

query_scalar() {
  local port=$1
  local database=$2
  local statement=$3
  psql_exec "$port" "$database" --tuples-only --no-align --quiet --command "$statement" |
    tail -n 1 | tr -d '[:space:]'
}

apply_migration() {
  local migration=$1
  [[ -f "$migration" && ! -L "$migration" ]] || die "unsafe migration path: $migration"
  psql_exec 5432 torgnexa --file - <"$migration" >/dev/null
}

apply_atomic_migration() {
  local version=$1 name=$2 file=$3 phase=$4 risk=$5 checksum=$6 migration=$7
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
    cat -- "$migration"
  } | psql_exec 5432 torgnexa --file - >/dev/null
}

seed_bootstrap_history() {
  local rows=$1 values= version name file phase risk checksum history_mode value
  while IFS=$'\t' read -r version name file phase risk checksum history_mode; do
    [[ "$history_mode" == bootstrap ]] || continue
    value="($version, '$name', '$file', '$phase', '$risk', '$checksum', '0.1.0', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670', 0)"
    if [[ -z "$values" ]]; then values=$value; else values="$values, $value"; fi
  done <<<"$rows"
  psql_exec 5432 torgnexa --command "
    INSERT INTO migration_history (
      version, name, file_name, phase, risk, checksum_sha256,
      application_version, execution_id, duration_ms
    ) VALUES $values;
  " >/dev/null
}

apply_catalog_migrations() {
  local rows version name file phase risk expected history_mode digest_line actual bootstrap_seeded=false
  rows="$(jq -er '.migrations[] | [.version, .name, .file, .phase, .risk, .sha256, .history_mode] | @tsv' "$repo_root/migrations/catalog.json")" || \
    die "unable to read migration catalog"
  while IFS=$'\t' read -r version name file phase risk expected history_mode; do
    [[ "$file" =~ ^[0-9]{6}_[a-z][a-z0-9_]{1,62}\.sql$ && "$expected" =~ ^[0-9a-f]{64}$ ]] || \
      die "unsafe migration catalog entry"
    digest_line="$(sha256sum -- "$repo_root/migrations/$file")"
    actual="${digest_line%% *}"
    [[ "$actual" == "$expected" ]] || die "migration checksum drift: $file"
    if [[ "$history_mode" == bootstrap ]]; then
      apply_migration "$repo_root/migrations/$file"
      continue
    fi
    if [[ "$bootstrap_seeded" != true ]]; then
      seed_bootstrap_history "$rows"
      bootstrap_seeded=true
    fi
    apply_atomic_migration "$version" "$name" "$file" "$phase" "$risk" "$expected" "$repo_root/migrations/$file"
  done <<<"$rows"
  if [[ "$bootstrap_seeded" != true ]]; then seed_bootstrap_history "$rows"; fi
}

psql_exec 5432 postgres --command "CREATE DATABASE torgnexa;" >/dev/null
apply_catalog_migrations

psql_exec 5432 torgnexa --command "
  CREATE ROLE torgnexa_restore_app NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
  GRANT SELECT ON organizations, workspaces, stores, outbox_events, inbox_receipts, secret_references, secret_versions, privacy_purposes, privacy_retention_policies, products, offers, connector_entity_mappings, prices, warehouses, inventory_positions, orders, order_items, lineage_records, lineage_inputs, pim_brands, pim_categories, pim_attributes, pim_product_brands, pim_product_categories, pim_product_attribute_values, pim_field_authorities, pim_duplicate_candidates, pim_merge_previews, legal_entities, individual_entrepreneurs, legal_branches, counterparties, legal_addresses, counterparty_bank_accounts, counterparty_contracts, counterparty_authorities, legal_party_duplicate_candidates, legal_party_merge_previews, compliance_documents, compliance_bindings, compliance_policies, compliance_verifications TO torgnexa_restore_app;
  CREATE TABLE backup_restore_markers (
    marker text PRIMARY KEY,
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
  );
  INSERT INTO organizations (id, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', 'Synthetic Organization A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', 'Synthetic Organization B');
  INSERT INTO workspaces (id, organization_id, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', 'Synthetic Workspace A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', 'Synthetic Workspace B');
  INSERT INTO stores (id, organization_id, workspace_id, code, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'synthetic-a', 'Synthetic Store A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0003', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'synthetic-b', 'Synthetic Store B');
  INSERT INTO connector_accounts (id, organization_id, workspace_id, family, provider, status) VALUES
    ('restore-connector-a', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'marketplace', 'synthetic', 'disabled'),
    ('restore-connector-b', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'marketplace', 'synthetic', 'disabled');
  INSERT INTO products (id, organization_id, workspace_id, code, title, description, status) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'RESTORE-A', 'Restore Product A', 'Catalog restore fixture', 'draft'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'RESTORE-B', 'Restore Product B', 'Catalog restore fixture', 'draft');
  UPDATE products SET status='active', version=2, updated_at=clock_timestamp() WHERE code IN ('RESTORE-A','RESTORE-B');
  INSERT INTO offers (id, organization_id, workspace_id, product_id, sku, gtin, status) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101', 'RESTORE-A-1', '4006381333931', 'draft'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0102', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101', 'RESTORE-B-1', '5012345678900', 'draft');
  UPDATE offers SET status='active', version=2, updated_at=clock_timestamp() WHERE sku IN ('RESTORE-A-1','RESTORE-B-1');
  INSERT INTO pim_brands(id,organization_id,workspace_id,code,name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0401','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','RESTORE-BRAND-A','Restore Brand A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0401','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','RESTORE-BRAND-B','Restore Brand B');
  INSERT INTO pim_categories(id,organization_id,workspace_id,code,name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0402','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','RESTORE-CAT-A','Restore Category A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0402','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','RESTORE-CAT-B','Restore Category B');
  INSERT INTO pim_attributes(id,organization_id,workspace_id,code,name,value_type) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0403','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','RESTORE-WEIGHT','Restore Weight','decimal'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0403','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','RESTORE-WEIGHT','Restore Weight','decimal');
  INSERT INTO pim_product_brands(organization_id,workspace_id,product_id,brand_id,source) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0401','restore.seed'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0401','restore.seed');
  INSERT INTO legal_entities(id,organization_id,workspace_id,code,legal_name,short_name,country_code,inn,kpp,ogrn,created_at,updated_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0501','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','RESTORE-LEGAL-A','Restore Legal A','Legal A','RU','7701234560','770101001','1027701234560',clock_timestamp(),clock_timestamp()),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0501','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','RESTORE-LEGAL-B','Restore Legal B','Legal B','RU','7801234564','780101001','1027801234560',clock_timestamp(),clock_timestamp());
  INSERT INTO counterparties(id,organization_id,workspace_id,code,party_type,party_id,role,created_at,updated_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0502','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','RESTORE-CP-A','legal_entity','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0501','supplier',clock_timestamp(),clock_timestamp()),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0502','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','RESTORE-CP-B','legal_entity','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0501','customer',clock_timestamp(),clock_timestamp());
  INSERT INTO compliance_documents(id,organization_id,workspace_id,document_type,number,jurisdiction,issuer,registry_source,status,issued_at,expires_at,holder_party_type,holder_party_id,version,created_at,updated_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0801','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','certificate','RESTORE-CERT-A','RU','Restore Registry','registry','draft','2026-08-01T00:00:00Z','2027-08-01T00:00:00Z','legal_entity','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0501',1,'2026-08-09T09:00:00Z','2026-08-09T09:00:00Z'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0801','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','certificate','RESTORE-CERT-B','RU','Restore Registry','registry','draft','2026-08-01T00:00:00Z','2027-08-01T00:00:00Z','legal_entity','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0501',1,'2026-08-09T09:00:00Z','2026-08-09T09:00:00Z');
  UPDATE compliance_documents SET status='valid',verification_source='registry',verified_at='2026-08-09T09:05:00Z',version=2,updated_at='2026-08-09T09:05:00Z' WHERE number IN ('RESTORE-CERT-A','RESTORE-CERT-B');
  INSERT INTO compliance_bindings(id,organization_id,workspace_id,document_id,subject_type,subject_id,active,version,created_at,updated_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0802','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0801','product','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101',true,1,'2026-08-09T09:06:00Z','2026-08-09T09:06:00Z'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0802','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0801','sku','RESTORE-B-1',true,1,'2026-08-09T09:06:00Z','2026-08-09T09:06:00Z');
  INSERT INTO compliance_policies(id,organization_id,workspace_id,code,jurisdiction,operation,requirements,effective_from,active,version,created_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0803','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','restore.publication','RU','publication','[{"document_type":"certificate","failure_outcome":"block","verification_required":true,"min_validity_hours":24}]'::jsonb,'2026-08-01T00:00:00Z',true,1,'2026-08-09T09:07:00Z'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0803','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','restore.publication','RU','publication','[{"document_type":"certificate","failure_outcome":"block","verification_required":true,"min_validity_hours":24}]'::jsonb,'2026-08-01T00:00:00Z',true,1,'2026-08-09T09:07:00Z');
  INSERT INTO connector_entity_mappings (organization_id, workspace_id, connector_account_id, entity_type, local_entity_id, remote_id) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'restore-connector-a', 'offer', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102', 'restore-remote-a'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'restore-connector-b', 'offer', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0102', 'restore-remote-b');
  INSERT INTO prices (id, organization_id, workspace_id, offer_id, kind, minor_units, currency) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0201', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102', 'regular', 12345, 'RUB'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0201', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0102', 'regular', 54321, 'RUB');
  INSERT INTO warehouses (id, organization_id, workspace_id, code, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0302', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'RESTORE-WH-A', 'Restore Warehouse A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0302', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'RESTORE-WH-B', 'Restore Warehouse B');
  INSERT INTO inventory_positions (id, organization_id, workspace_id, offer_id, warehouse_id, unit) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0301', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0302', 'EA'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0301', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0102', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0302', 'EA');
  INSERT INTO orders(id,organization_id,workspace_id,order_number,currency,subtotal_minor_units,discount_minor_units,tax_minor_units,shipping_minor_units,grand_minor_units,placed_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0601','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','RESTORE-ORDER-A','RUB',1000,100,180,100,1180,'2026-08-09T09:00:00Z'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0601','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','RESTORE-ORDER-B','RUB',2000,0,400,0,2400,'2026-08-09T09:00:00Z');
  INSERT INTO order_items(id,organization_id,workspace_id,order_id,position,offer_id,sku_snapshot,quantity_coefficient,quantity_scale,unit,unit_price_minor_units,subtotal_minor_units,discount_minor_units,tax_minor_units,line_total_minor_units,tax_jurisdiction,tax_category,tax_rate_coefficient,tax_rate_scale,price_includes_tax) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0602','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0601',1,'018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102','RESTORE-A-1',1,0,'EA',1000,1000,100,180,1080,'RU','standard',2,1,false),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0602','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0601',1,'018f0e8b-8a58-7f42-8c2d-5c2f9b1b0102','RESTORE-B-1',2,0,'EA',1000,2000,0,400,2400,'RU','standard',2,1,false);
  INSERT INTO outbox_events (id, organization_id, workspace_id, event_type, aggregate_type, aggregate_id, payload, event_envelope) VALUES
    ('evt_restore_a', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'commerce.orders.order_created.v1', 'order', 'order_restore_a', '{"order_id":"order_restore_a"}'::jsonb, '{"event_id":"evt_restore_a","event_type":"commerce.orders.order_created.v1","occurred_at":"2026-08-09T09:00:00Z","organization_id":"018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001","workspace_id":"018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002","correlation_id":null,"causation_id":null,"entity_type":"order","entity_id":"order_restore_a","source":"restore","data":{"order_id":"order_restore_a"}}'::jsonb),
    ('evt_restore_b', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'commerce.orders.order_created.v1', 'order', 'order_restore_b', '{"order_id":"order_restore_b"}'::jsonb, '{"event_id":"evt_restore_b","event_type":"commerce.orders.order_created.v1","occurred_at":"2026-08-09T09:00:00Z","organization_id":"018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001","workspace_id":"018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002","correlation_id":null,"causation_id":null,"entity_type":"order","entity_id":"order_restore_b","source":"restore","data":{"order_id":"order_restore_b"}}'::jsonb);
  INSERT INTO audit_records (id,organization_id,workspace_id,actor_id,source,action,resource_type,resource_id,correlation_id,risk,summary,created_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0710','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','system','restore','lineage.seed','price','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0201','restore-a','write_safe','{}'::jsonb,'2026-08-09T09:00:00Z'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0710','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','system','restore','lineage.seed','price','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0201','restore-b','write_safe','{}'::jsonb,'2026-08-09T09:00:00Z');
  INSERT INTO lineage_records (id,organization_id,workspace_id,source,actor_id,operation,output_system,output_entity_type,output_entity_id,output_entity_version,output_field,transform_kind,transform_id,transform_version,correlation_id,audit_id,event_id,result,fingerprint_sha256,occurred_at) VALUES
    ('lin.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa01','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','restore','system','pricing.price.created','torgnexa','price','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0201','1','amount','restore_seed','restore.lineage','1','restore-a','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0710','evt_restore_a','observed',repeat('a',64),'2026-08-09T09:00:00Z'),
    ('lin.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb02','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','restore','system','pricing.price.created','torgnexa','price','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0201','1','amount','restore_seed','restore.lineage','1','restore-b','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0710','evt_restore_b','observed',repeat('b',64),'2026-08-09T09:00:00Z');
  INSERT INTO secret_references (reference, organization_id, workspace_id, class, status, current_version) VALUES
    ('sec:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'connector_token', 'active', 1),
    ('sec:v1:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'connector_token', 'active', 1);
  INSERT INTO secret_versions (reference, organization_id, workspace_id, version, algorithm, key_id, nonce, ciphertext) VALUES
    ('sec:v1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 1, 'aes-256-gcm', 'restore-k1', decode(repeat('11',12),'hex'), decode(repeat('aa',32),'hex')),
    ('sec:v1:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 1, 'aes-256-gcm', 'restore-k1', decode(repeat('22',12),'hex'), decode(repeat('bb',32),'hex'));
  INSERT INTO privacy_purposes (organization_id, workspace_id, purpose_key, description, legal_basis, notice_reference, consent_reference, allowed_classes) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'order_fulfillment', 'Fulfil synthetic customer orders', 'contract', 'privacy-notice:v1', '', '["personal"]'::jsonb),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'order_fulfillment', 'Fulfil synthetic customer orders', 'contract', 'privacy-notice:v1', '', '["personal"]'::jsonb);
  INSERT INTO privacy_retention_policies (organization_id, workspace_id, purpose_key, data_class, retention_days, disposition, legal_hold_permitted) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'order_fulfillment', 'personal', 365, 'anonymize', true),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'order_fulfillment', 'personal', 365, 'anonymize', true);
  INSERT INTO inbox_receipts (organization_id, workspace_id, consumer, event_id, event_type, event_fingerprint, first_observed_at, processed_attempt) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'orders.restore.v1', 'evt_inbox_restore_a', 'commerce.orders.order_created.v1', repeat('a',64), '2026-08-09T10:00:00Z', 1),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'orders.restore.v1', 'evt_inbox_restore_b', 'commerce.orders.order_created.v1', repeat('b',64), '2026-08-09T10:00:00Z', 1);
  INSERT INTO backup_restore_markers (marker) VALUES ('base-backup-visible');
" >/dev/null

started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
drill_stamp="$(date -u +%Y%m%dT%H%M%SZ)"
drill_id="postgres-restore-${drill_stamp}-${BASHPID}"

docker_exec pg_dump \
  --host /tmp --port 5432 --username postgres --dbname torgnexa \
  --format custom --compress zstd:9 --no-owner --file /tmp/logical.dump
docker_exec pg_restore --list /tmp/logical.dump >/dev/null
docker_exec createdb --host /tmp --port 5432 --username postgres torgnexa_logical
docker_exec pg_restore \
  --host /tmp --port 5432 --username postgres --dbname torgnexa_logical \
  --exit-on-error --single-transaction --no-owner /tmp/logical.dump

logical_markers="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM backup_restore_markers WHERE marker = 'base-backup-visible';")"
[[ "$logical_markers" == 1 ]] || die "logical restore lost the pre-backup marker"
logical_stores="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM stores;")"
[[ "$logical_stores" == 2 ]] || die "logical restore returned $logical_stores stores instead of two"
logical_secrets="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM secret_versions;")"
[[ "$logical_secrets" == 2 ]] || die "logical restore lost encrypted secret versions"
logical_outbox="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM outbox_events WHERE event_envelope IS NOT NULL;")"
[[ "$logical_outbox" == 2 ]] || die "logical restore lost transactional outbox event intents"
logical_inbox="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM inbox_receipts;")"
[[ "$logical_inbox" == 2 ]] || die "logical restore lost immutable inbox receipts"
logical_products="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM products;")"
[[ "$logical_products" == 2 ]] || die "logical restore lost canonical products"
logical_offers="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM offers;")"
[[ "$logical_offers" == 2 ]] || die "logical restore lost canonical offers"
logical_mappings="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM connector_entity_mappings;")"
[[ "$logical_mappings" == 2 ]] || die "logical restore lost connector entity mappings"
logical_pim="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM pim_brands;")"
[[ "$logical_pim" == 2 ]] || die "logical restore lost canonical PIM brands"
logical_legal_entities="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM legal_entities;")"
[[ "$logical_legal_entities" == 2 ]] || die "logical restore lost canonical legal entities"
logical_counterparties="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM counterparties;")"
[[ "$logical_counterparties" == 2 ]] || die "logical restore lost canonical counterparties"
logical_compliance_documents="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM compliance_documents;")"
[[ "$logical_compliance_documents" == 2 ]] || die "logical restore lost compliance documents"
logical_compliance_policies="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM compliance_policies;")"
[[ "$logical_compliance_policies" == 2 ]] || die "logical restore lost compliance policies"
logical_prices="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM prices;")"
[[ "$logical_prices" == 2 ]] || die "logical restore lost canonical prices"
logical_inventory="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM inventory_positions;")"
logical_orders="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM orders;")"
logical_order_items="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM order_items;")"
[[ "$logical_inventory" == 2 ]] || die "logical restore lost inventory positions"
[[ "$logical_orders" == 2 ]] || die "logical restore lost canonical orders"
[[ "$logical_order_items" == 2 ]] || die "logical restore lost immutable order items"
logical_lineage="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM lineage_records;")"
[[ "$logical_lineage" == 2 ]] || die "logical restore lost immutable lineage evidence"
logical_rls="$(query_scalar 5432 torgnexa_logical "
  BEGIN READ ONLY;
  SET LOCAL ROLE torgnexa_restore_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  SELECT count(*) FROM stores;
  ROLLBACK;
")"
[[ "$logical_rls" == 1 ]] || die "logical restore did not preserve tenant RLS"
logical_outbox_rls="$(query_scalar 5432 torgnexa_logical "
  BEGIN READ ONLY;
  SET LOCAL ROLE torgnexa_restore_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  SELECT count(*) FROM outbox_events WHERE event_envelope IS NOT NULL;
  ROLLBACK;
")"
[[ "$logical_outbox_rls" == 1 ]] || die "logical restore did not preserve outbox tenant RLS"
logical_inbox_rls="$(query_scalar 5432 torgnexa_logical "
  BEGIN READ ONLY;
  SET LOCAL ROLE torgnexa_restore_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  SELECT count(*) FROM inbox_receipts;
  ROLLBACK;
")"
[[ "$logical_inbox_rls" == 1 ]] || die "logical restore did not preserve inbox tenant RLS"
logical_catalog_rls="$(query_scalar 5432 torgnexa_logical "
  BEGIN READ ONLY;
  SET LOCAL ROLE torgnexa_restore_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  SELECT (SELECT count(*) FROM products) + (SELECT count(*) FROM offers) + (SELECT count(*) FROM connector_entity_mappings) + (SELECT count(*) FROM prices) + (SELECT count(*) FROM warehouses) + (SELECT count(*) FROM inventory_positions);
  ROLLBACK;
")"
[[ "$logical_catalog_rls" == 6 ]] || die "logical restore did not preserve commerce-core tenant RLS"
logical_lineage_rls="$(query_scalar 5432 torgnexa_logical "
  BEGIN READ ONLY;
  SET LOCAL ROLE torgnexa_restore_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  SELECT count(*) FROM lineage_records;
  ROLLBACK;
")"
[[ "$logical_lineage_rls" == 1 ]] || die "logical restore did not preserve lineage tenant RLS"
logical_secret_rls="$(query_scalar 5432 torgnexa_logical "
  BEGIN READ ONLY;
  SET LOCAL ROLE torgnexa_restore_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  SELECT count(*) FROM secret_references;
  ROLLBACK;
")"
[[ "$logical_secret_rls" == 1 ]] || die "logical restore did not preserve secret RLS"
logical_privacy_rows="$(query_scalar 5432 torgnexa_logical "SELECT count(*) FROM privacy_retention_policies;")"
[[ "$logical_privacy_rows" == 2 ]] || die "logical restore lost privacy retention metadata"
logical_privacy_rls="$(query_scalar 5432 torgnexa_logical "
  BEGIN READ ONLY;
  SET LOCAL ROLE torgnexa_restore_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  SELECT count(*) FROM privacy_purposes;
  ROLLBACK;
")"
[[ "$logical_privacy_rls" == 1 ]] || die "logical restore did not preserve privacy registry RLS"

docker_exec pg_basebackup \
  --host /tmp --port 5432 --username postgres --pgdata /tmp/base \
  --format plain --checkpoint fast --wal-method stream \
  --manifest-checksums SHA256 --no-password
docker_exec pg_verifybackup /tmp/base >/dev/null

postgres_version="$(query_scalar 5432 postgres "SHOW server_version;")"
[[ "$postgres_version" =~ ^[0-9]+\.[0-9]+(\.[0-9]+)?$ ]] || die "unexpected PostgreSQL version: $postgres_version"
source_timeline="$(query_scalar 5432 postgres "SELECT timeline_id FROM pg_control_checkpoint();")"
[[ "$source_timeline" =~ ^[1-9][0-9]*$ ]] || die "unexpected source timeline: $source_timeline"

logical_sha_line="$(docker_exec sha256sum /tmp/logical.dump)"
logical_sha="${logical_sha_line%% *}"
logical_size="$(docker_exec stat -c %s /tmp/logical.dump)"
base_manifest_sha_line="$(docker_exec sha256sum /tmp/base/backup_manifest)"
base_manifest_sha="${base_manifest_sha_line%% *}"
base_manifest_size="$(docker_exec stat -c %s /tmp/base/backup_manifest)"
[[ "$logical_sha" =~ ^[0-9a-f]{64}$ && "$base_manifest_sha" =~ ^[0-9a-f]{64}$ ]] || die "backup checksum generation failed"
[[ "$logical_size" =~ ^[1-9][0-9]*$ && "$base_manifest_size" =~ ^[1-9][0-9]*$ ]] || die "backup artifact is empty"

psql_exec 5432 torgnexa --command "INSERT INTO backup_restore_markers (marker) VALUES ('at-recovery-target');" >/dev/null
target_lsn="$(query_scalar 5432 postgres "SELECT pg_current_wal_flush_lsn();")"
[[ "$target_lsn" =~ ^[0-9A-F]+/[0-9A-F]+$ ]] || die "unexpected recovery target LSN: $target_lsn"
psql_exec 5432 torgnexa --command "INSERT INTO backup_restore_markers (marker) VALUES ('after-recovery-target');" >/dev/null
last_required_segment="$(query_scalar 5432 postgres "SELECT pg_walfile_name(pg_current_wal_lsn());")"
[[ "$last_required_segment" =~ ^[0-9A-F]{24}$ ]] || die "unexpected WAL segment name: $last_required_segment"
psql_exec 5432 postgres --command "SELECT pg_switch_wal();" >/dev/null

wal_archived=false
for _ in {1..30}; do
  if docker_exec test -s "/tmp/wal_archive/$last_required_segment"; then
    wal_archived=true
    break
  fi
  sleep 1
done
[[ "$wal_archived" == true ]] || die "required WAL segment was not archived"

docker_exec pg_ctl --pgdata=/tmp/primary --wait --timeout=30 --mode fast stop >/dev/null
docker_exec cp -a /tmp/base /tmp/recovery
docker_exec sh -eu -c '
  printf "\ncorruption-test\n" >>/tmp/base/PG_VERSION
'
if docker_exec pg_verifybackup /tmp/base >/dev/null 2>&1; then
  die "pg_verifybackup accepted a deliberately corrupted base backup"
fi

docker_exec sh -eu -c '
  : >/tmp/recovery/recovery.signal
  printf "%s\n" "$@" >>/tmp/recovery/postgresql.auto.conf
' sh \
  "archive_mode = off" \
  "restore_command = 'test -f /tmp/wal_archive/%f && cp /tmp/wal_archive/%f %p'" \
  "recovery_target_lsn = '$target_lsn'" \
  "recovery_target_inclusive = on" \
  "recovery_target_action = promote"

if ! docker_exec pg_ctl --pgdata=/tmp/recovery --wait --timeout=30 --log=/tmp/recovery.log start; then
  if ! docker_exec tail -n 120 /tmp/recovery.log >&2; then
    echo "check-postgres-backup-restore: recovery log is unavailable" >&2
  fi
  die "PITR cluster failed to start"
fi

recovery_done="$(query_scalar 5432 postgres "SELECT (NOT pg_is_in_recovery())::text;")"
[[ "$recovery_done" == t || "$recovery_done" == true ]] || die "PITR cluster did not promote at the target LSN"
base_visible="$(query_scalar 5432 torgnexa "SELECT count(*) FROM backup_restore_markers WHERE marker = 'base-backup-visible';")"
target_visible="$(query_scalar 5432 torgnexa "SELECT count(*) FROM backup_restore_markers WHERE marker = 'at-recovery-target';")"
post_target_visible="$(query_scalar 5432 torgnexa "SELECT count(*) FROM backup_restore_markers WHERE marker = 'after-recovery-target';")"
[[ "$base_visible" == 1 ]] || die "physical restore lost the base-backup marker"
[[ "$target_visible" == 1 ]] || die "PITR excluded the inclusive target marker"
[[ "$post_target_visible" == 0 ]] || die "PITR replayed a post-target marker"

physical_rls="$(query_scalar 5432 torgnexa "
  BEGIN READ ONLY;
  SET LOCAL ROLE torgnexa_restore_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  SELECT count(*) FROM stores;
  ROLLBACK;
")"
[[ "$physical_rls" == 1 ]] || die "physical PITR did not preserve tenant RLS"
physical_secret_rows="$(query_scalar 5432 torgnexa "SELECT count(*) FROM secret_versions;")"
[[ "$physical_secret_rows" == 2 ]] || die "physical PITR lost encrypted secret versions"
physical_outbox_rows="$(query_scalar 5432 torgnexa "SELECT count(*) FROM outbox_events WHERE event_envelope IS NOT NULL;")"
[[ "$physical_outbox_rows" == 2 ]] || die "physical PITR lost transactional outbox event intents"
physical_inbox_rows="$(query_scalar 5432 torgnexa "SELECT count(*) FROM inbox_receipts;")"
[[ "$physical_inbox_rows" == 2 ]] || die "physical PITR lost immutable inbox receipts"
physical_products="$(query_scalar 5432 torgnexa "SELECT count(*) FROM products;")"
[[ "$physical_products" == 2 ]] || die "physical PITR lost canonical products"
physical_offers="$(query_scalar 5432 torgnexa "SELECT count(*) FROM offers;")"
[[ "$physical_offers" == 2 ]] || die "physical PITR lost canonical offers"
physical_prices="$(query_scalar 5432 torgnexa "SELECT count(*) FROM prices;")"
[[ "$physical_prices" == 2 ]] || die "physical PITR lost canonical prices"
physical_inventory="$(query_scalar 5432 torgnexa "SELECT count(*) FROM inventory_positions;")"
physical_lineage="$(query_scalar 5432 torgnexa "SELECT count(*) FROM lineage_records;")"
physical_orders="$(query_scalar 5432 torgnexa "SELECT count(*) FROM orders;")"
physical_order_items="$(query_scalar 5432 torgnexa "SELECT count(*) FROM order_items;")"
[[ "$physical_inventory" == 2 ]] || die "physical PITR lost inventory positions"
[[ "$physical_lineage" == 2 ]] || die "physical PITR lost lineage evidence"
[[ "$physical_orders" == 2 ]] || die "physical PITR lost canonical orders"
[[ "$physical_order_items" == 2 ]] || die "physical PITR lost immutable order items"
physical_mappings="$(query_scalar 5432 torgnexa "SELECT count(*) FROM connector_entity_mappings;")"
[[ "$physical_mappings" == 2 ]] || die "physical PITR lost connector entity mappings"
physical_pim="$(query_scalar 5432 torgnexa "SELECT count(*) FROM pim_brands;")"
[[ "$physical_pim" == 2 ]] || die "physical PITR lost canonical PIM brands"
physical_legal_entities="$(query_scalar 5432 torgnexa "SELECT count(*) FROM legal_entities;")"
[[ "$physical_legal_entities" == 2 ]] || die "physical PITR lost canonical legal entities"
physical_counterparties="$(query_scalar 5432 torgnexa "SELECT count(*) FROM counterparties;")"
[[ "$physical_counterparties" == 2 ]] || die "physical PITR lost canonical counterparties"
physical_compliance_documents="$(query_scalar 5432 torgnexa "SELECT count(*) FROM compliance_documents;")"
[[ "$physical_compliance_documents" == 2 ]] || die "physical PITR lost compliance documents"
physical_compliance_policies="$(query_scalar 5432 torgnexa "SELECT count(*) FROM compliance_policies;")"
[[ "$physical_compliance_policies" == 2 ]] || die "physical PITR lost compliance policies"
physical_connector_sdk_rows="$(query_scalar 5432 torgnexa "SELECT count(*) FROM connector_accounts WHERE version >= 1 AND health_status='unknown';")"
[[ "$physical_connector_sdk_rows" == 2 ]] || die "physical PITR lost Task-010 connector account metadata"
physical_privacy_rows="$(query_scalar 5432 torgnexa "SELECT count(*) FROM privacy_retention_policies;")"
[[ "$physical_privacy_rows" == 2 ]] || die "physical PITR lost privacy retention metadata"

docker_exec pg_ctl --pgdata=/tmp/recovery --wait --timeout=30 --mode fast stop >/dev/null
completed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

evidence_json="$(jq -cn \
  --arg drill_id "$drill_id" \
  --arg started_at "$started_at" \
  --arg completed_at "$completed_at" \
  --arg image "$postgres_image" \
  --arg server_version "$postgres_version" \
  --argjson timeline "$source_timeline" \
  --arg logical_sha "$logical_sha" \
  --argjson logical_size "$logical_size" \
  --arg manifest_sha "$base_manifest_sha" \
  --argjson manifest_size "$base_manifest_size" \
  --arg target_lsn "$target_lsn" \
  --arg last_segment "$last_required_segment" '
  {
    schema_version: 1,
    drill_id: $drill_id,
    environment: "synthetic_ci",
    started_at: $started_at,
    completed_at: $completed_at,
    postgres: {
      image: $image,
      server_version: $server_version,
      source_timeline: $timeline
    },
    artifacts: [
      {name: "logical.dump", kind: "postgres_custom_dump", sha256: $logical_sha, size_bytes: $logical_size},
      {name: "backup_manifest", kind: "postgres_base_backup_manifest", sha256: $manifest_sha, size_bytes: $manifest_size}
    ],
    recovery_target: {kind: "lsn", value: $target_lsn, inclusive: true},
    wal_archive: {timeline: $timeline, last_required_segment: $last_segment, durability: "ephemeral_test"},
    checks: {
      logical_restore: "passed",
      base_backup_manifest: "passed",
      physical_restore: "passed",
      pitr_target_included: "passed",
      pitr_post_target_excluded: "passed",
      row_level_security: "passed",
      corruption_detection: "passed"
    },
    result: "passed"
  }
')"

printf '%s\n' "$evidence_json" | jq -e '
  .schema_version == 1 and
  .environment == "synthetic_ci" and
  .result == "passed" and
  (.artifacts | length == 2) and
  ([.checks[]] | all(. == "passed"))
' >/dev/null

if [[ -n "$evidence_file" ]]; then
  evidence_tmp="$(mktemp "$(dirname -- "$evidence_file")/.postgres-restore-evidence.XXXXXX")"
  printf '%s\n' "$evidence_json" >"$evidence_tmp"
  chmod 0600 "$evidence_tmp"
  mv -- "$evidence_tmp" "$evidence_file"
  evidence_tmp=
  evidence_sha_line="$(sha256sum -- "$evidence_file")"
  evidence_sha="${evidence_sha_line%% *}"
  echo "PostgreSQL logical restore and physical PITR smoke passed; evidence=$evidence_file sha256=$evidence_sha"
else
  echo "PostgreSQL logical restore and physical PITR smoke passed"
fi
