#!/usr/bin/env bash
set -euo pipefail

umask 077
export LC_ALL=C
export TZ=UTC

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
inventory="$repo_root/supply-chain/release-artifacts.json"
container_name="torgnexa-tenancy-smoke-${BASHPID}"
database_name=torgnexa
database_user=torgnexa
database_password=synthetic-local-smoke-only
container_started=false

die() {
  echo "check-tenancy-postgres: $*" >&2
  exit 1
}

for command_name in docker jq mktemp rm sha256sum tail tr; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done

[[ "$container_name" =~ ^torgnexa-tenancy-smoke-[0-9]+$ ]] || die "unsafe container name"
postgres_image="$(jq -er '.development_runtime[] | select(.name == "postgres") | .image' "$inventory")" || \
  die "PostgreSQL runtime image is not registered exactly once"
[[ "$postgres_image" =~ ^postgres:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$ ]] || \
  die "PostgreSQL runtime image is not immutable"
[[ -z "$(docker ps --all --filter "name=^/${container_name}$" --format '{{.Names}}')" ]] || \
  die "temporary container name already exists"

cleanup() {
  if [[ "$container_started" == true ]]; then
    docker stop --time 5 "$container_name" >/dev/null
  fi
}
trap cleanup EXIT HUP INT TERM

docker run --rm --detach \
  --name "$container_name" \
  --network none \
  --memory 512m \
  --cpus 1 \
  --pids-limit 256 \
  --env "POSTGRES_DB=$database_name" \
  --env "POSTGRES_USER=$database_user" \
  --env "POSTGRES_PASSWORD=$database_password" \
  "$postgres_image" >/dev/null
container_started=true

ready=false
for _ in {1..30}; do
  if docker exec \
    --env "PGPASSWORD=$database_password" \
    "$container_name" \
    psql --no-psqlrc --set ON_ERROR_STOP=1 --quiet \
    --username "$database_user" --dbname "$database_name" \
    --command 'SELECT 1;' >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 1
done
if [[ "$ready" != true ]]; then
  if ! docker logs --tail 80 "$container_name" >&2; then
    echo "check-tenancy-postgres: unable to read temporary container logs" >&2
  fi
  die "temporary PostgreSQL did not become ready"
fi

psql_exec() {
  local target_database=$1
  shift
  docker exec --interactive \
    --env "PGPASSWORD=$database_password" \
    "$container_name" \
    psql --no-psqlrc --set ON_ERROR_STOP=1 \
    --username "$database_user" --dbname "$target_database" "$@"
}

apply_migration() {
  local target_database=$1
  local migration=$2
  [[ -f "$migration" && ! -L "$migration" ]] || die "unsafe migration path: $migration"
  psql_exec "$target_database" --file - <"$migration" >/dev/null
}

apply_atomic_migration() {
  local target_database=$1 version=$2 name=$3 file=$4 phase=$5 risk=$6 checksum=$7 migration=$8
  local migration_input
  [[ "$version" =~ ^[0-9]+$ && "$name" =~ ^[a-z][a-z0-9_]{1,62}$ && "$file" =~ ^[0-9]{6}_[a-z][a-z0-9_]{1,62}\.sql$ ]] || \
    die "unsafe atomic migration metadata"
  [[ "$phase" =~ ^(expand|migrate|contract)$ && "$risk" =~ ^(low|medium|high|critical)$ && "$checksum" =~ ^[0-9a-f]{64}$ ]] || \
    die "unsafe atomic migration policy metadata"
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
  } >"${migration_input:=$(mktemp)}"
  if ! psql_exec "$target_database" --file - <"$migration_input" >/dev/null; then
    rm -f -- "$migration_input"
    return 1
  fi
  rm -f -- "$migration_input"
}

seed_bootstrap_history() {
  local target_database=$1 rows=$2 values= version name file phase risk checksum history_mode value
  while IFS=$'\t' read -r version name file phase risk checksum history_mode; do
    [[ "$history_mode" == bootstrap ]] || continue
    value="($version, '$name', '$file', '$phase', '$risk', '$checksum', '0.1.0', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670', 0)"
    if [[ -z "$values" ]]; then values=$value; else values="$values, $value"; fi
  done < <(printf '%s\n' "$rows")
  [[ -n "$values" ]] || die "bootstrap migration history is empty"
  psql_exec "$target_database" --command "
    INSERT INTO migration_history (
      version, name, file_name, phase, risk, checksum_sha256,
      application_version, execution_id, duration_ms
    ) VALUES $values;
  " </dev/null >/dev/null
}

apply_catalog_migrations() {
  local target_database=$1 rows version name file phase risk expected history_mode digest_line actual bootstrap_seeded=false
  rows="$(jq -er '.migrations[] | [.version, .name, .file, .phase, .risk, .sha256, .history_mode] | @tsv' "$repo_root/migrations/catalog.json")" || \
    die "unable to read migration catalog"
  while IFS=$'\t' read -r version name file phase risk expected history_mode; do
    [[ "$file" =~ ^[0-9]{6}_[a-z][a-z0-9_]{1,62}\.sql$ && "$expected" =~ ^[0-9a-f]{64}$ ]] || \
      die "unsafe migration catalog entry"
    digest_line="$(sha256sum -- "$repo_root/migrations/$file")"
    actual="${digest_line%% *}"
    [[ "$actual" == "$expected" ]] || die "migration checksum drift: $file"
    if [[ "$history_mode" == bootstrap ]]; then
      apply_migration "$target_database" "$repo_root/migrations/$file"
      continue
    fi
    if [[ "$bootstrap_seeded" != true ]]; then
      seed_bootstrap_history "$target_database" "$rows"
      bootstrap_seeded=true
    fi
    [[ "$history_mode" == atomic ]] || die "unsupported history mode: $history_mode"
    apply_atomic_migration "$target_database" "$version" "$name" "$file" "$phase" "$risk" "$expected" "$repo_root/migrations/$file"
  done < <(printf '%s\n' "$rows")
  if [[ "$bootstrap_seeded" != true ]]; then
    seed_bootstrap_history "$target_database" "$rows"
  fi
}

query_scalar() {
  local target_database=$1
  local statement=$2
  psql_exec "$target_database" --tuples-only --no-align --quiet --command "$statement" </dev/null |
    tail -n 1 | tr -d '[:space:]'
}

apply_catalog_migrations "$database_name"

psql_exec "$database_name" --command "
  CREATE ROLE torgnexa_app NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
  GRANT SELECT, INSERT, UPDATE, DELETE ON organizations, workspaces, stores TO torgnexa_app;
  GRANT SELECT, INSERT, UPDATE ON outbox_events TO torgnexa_app;
  GRANT SELECT, INSERT ON inbox_receipts TO torgnexa_app;
  GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON audit_records TO torgnexa_app;
  GRANT SELECT, INSERT, UPDATE ON connector_accounts, secret_references TO torgnexa_app;
  GRANT SELECT, INSERT ON secret_versions TO torgnexa_app;
  GRANT SELECT, INSERT, UPDATE ON privacy_purposes, privacy_retention_policies, user_profiles TO torgnexa_app;
  GRANT SELECT, INSERT, UPDATE ON products, offers, connector_entity_mappings, prices, warehouses, inventory_positions, orders TO torgnexa_app;
  GRANT SELECT, INSERT ON order_items, lineage_records, lineage_inputs TO torgnexa_app;
  GRANT SELECT, INSERT, UPDATE ON pim_brands, pim_categories, pim_attributes, pim_product_brands, pim_product_categories, pim_product_attribute_values, pim_field_authorities, pim_duplicate_candidates TO torgnexa_app;
  GRANT SELECT, INSERT ON pim_merge_previews TO torgnexa_app;
  GRANT SELECT, INSERT, UPDATE ON legal_entities, individual_entrepreneurs, legal_branches, counterparties, legal_addresses, counterparty_bank_accounts, counterparty_contracts, counterparty_authorities, legal_party_duplicate_candidates TO torgnexa_app;
  GRANT SELECT, INSERT ON legal_party_merge_previews TO torgnexa_app;
  GRANT SELECT, INSERT, UPDATE ON compliance_documents, compliance_bindings TO torgnexa_app;
  GRANT SELECT, INSERT ON compliance_policies, compliance_verifications TO torgnexa_app;
  INSERT INTO organizations (id, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', 'Synthetic Organization A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', 'Synthetic Organization B');
  INSERT INTO workspaces (id, organization_id, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', 'Synthetic Workspace A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', 'Synthetic Workspace B');
  INSERT INTO stores (id, organization_id, workspace_id, code, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'synthetic-a', 'Synthetic Store A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0003', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'synthetic-b', 'Synthetic Store B');
  INSERT INTO user_profiles (organization_id, workspace_id, subject_ref, username, email, given_name, family_name, job_title, department, phone_number)
  VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', repeat('a', 64), 'synthetic-a', 'synthetic-a@example.test', 'Synthetic', 'Operator A', 'Operator', 'Operations', '+70000000001'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', repeat('b', 64), 'synthetic-b', 'synthetic-b@example.test', 'Synthetic', 'Operator B', 'Operator', 'Operations', '+70000000002');
  INSERT INTO connector_accounts (id, organization_id, workspace_id, family, provider, status) VALUES
    ('connector-map-a', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'marketplace', 'synthetic', 'disabled'),
    ('connector-map-b', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'marketplace', 'synthetic', 'disabled');
  INSERT INTO products (id, organization_id, workspace_id, code, title, description, status) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'SYNTH-A', 'Synthetic Product A', 'Catalog seed', 'draft'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'SYNTH-B', 'Synthetic Product B', 'Catalog seed', 'draft');
  INSERT INTO offers (id, organization_id, workspace_id, product_id, sku, gtin, status) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101', 'SYNTH-A-1', '4006381333931', 'draft'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0102', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101', 'SYNTH-B-1', '5012345678900', 'draft');
  INSERT INTO pim_brands (id,organization_id,workspace_id,code,name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0401','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','BRAND-A','Brand A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0401','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','BRAND-B','Brand B');
  INSERT INTO pim_categories (id,organization_id,workspace_id,code,name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0402','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','CAT-A','Category A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0402','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','CAT-B','Category B');
  INSERT INTO pim_attributes (id,organization_id,workspace_id,code,name,value_type) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0403','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','WEIGHT','Weight','decimal'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0403','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','WEIGHT','Weight','decimal');
  INSERT INTO pim_product_brands(organization_id,workspace_id,product_id,brand_id,source) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0401','import.seed'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0401','import.seed');
  INSERT INTO pim_product_categories(organization_id,workspace_id,product_id,category_id,is_primary,source) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0402',true,'import.seed'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0402',true,'import.seed');
  INSERT INTO pim_product_attribute_values(organization_id,workspace_id,product_id,attribute_id,ordinal,value,source) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0403',0,to_jsonb('12.345'::text),'import.seed'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0403',0,to_jsonb('9.500'::text),'import.seed');
  INSERT INTO legal_entities(id,organization_id,workspace_id,code,legal_name,short_name,country_code,inn,kpp,ogrn,created_at,updated_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0501','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','LEGAL-A','Synthetic Legal A','Legal A','RU','7701234560','770101001','1027701234560',clock_timestamp(),clock_timestamp()),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0501','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','LEGAL-B','Synthetic Legal B','Legal B','RU','7801234564','780101001','1027801234560',clock_timestamp(),clock_timestamp());
  INSERT INTO counterparties(id,organization_id,workspace_id,code,party_type,party_id,role,created_at,updated_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0502','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','CP-A','legal_entity','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0501','supplier',clock_timestamp(),clock_timestamp()),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0502','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','CP-B','legal_entity','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0501','customer',clock_timestamp(),clock_timestamp());
  INSERT INTO legal_addresses(id,organization_id,workspace_id,party_type,party_id,kind,country_code,postal_code,city,line1,is_primary,created_at,updated_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0503','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','legal_entity','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0501','legal','RU','101000','Moscow','Synthetic Street 1',true,clock_timestamp(),clock_timestamp()),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0503','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','legal_entity','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0501','legal','RU','190000','Saint Petersburg','Synthetic Street 2',true,clock_timestamp(),clock_timestamp());
  INSERT INTO connector_entity_mappings (organization_id, workspace_id, connector_account_id, entity_type, local_entity_id, remote_id) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'connector-map-a', 'product', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101', 'remote-product-a'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'connector-map-b', 'product', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101', 'remote-product-b'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'connector-map-a', 'category', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0402', 'remote-category-a'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'connector-map-b', 'category', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0402', 'remote-category-b');
  INSERT INTO prices (id, organization_id, workspace_id, offer_id, kind, minor_units, currency) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0201', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102', 'regular', 12345, 'RUB'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0201', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0102', 'regular', 54321, 'RUB');
  INSERT INTO warehouses (id, organization_id, workspace_id, code, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0302', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'WH-A', 'Warehouse A'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0302', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'WH-B', 'Warehouse B');
  INSERT INTO inventory_positions (id, organization_id, workspace_id, offer_id, warehouse_id, unit) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0301', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0302', 'EA'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0301', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0102', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0302', 'EA');
  INSERT INTO audit_records (
    id, organization_id, workspace_id, actor_id, source, action,
    resource_type, resource_id, correlation_id, risk, summary
  ) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0010', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',
     'system', 'test', 'audit.seed', 'store', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003', 'seed-a', 'write_safe', '{\"status\":\"created\"}'::jsonb),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0010', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002',
     'system', 'test', 'audit.seed', 'store', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0003', 'seed-b', 'write_safe', '{\"status\":\"created\"}'::jsonb);
  INSERT INTO secret_references (reference, organization_id, workspace_id, class, status, current_version) VALUES
    ('sec:v1:11111111111111111111111111111111', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'connector_token', 'active', 1),
    ('sec:v1:22222222222222222222222222222222', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'connector_token', 'active', 1);
  INSERT INTO secret_versions (reference, organization_id, workspace_id, version, algorithm, key_id, nonce, ciphertext) VALUES
    ('sec:v1:11111111111111111111111111111111', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 1, 'aes-256-gcm', 'synthetic-k1', decode(repeat('11', 12), 'hex'), decode(repeat('aa', 32), 'hex')),
    ('sec:v1:22222222222222222222222222222222', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 1, 'aes-256-gcm', 'synthetic-k1', decode(repeat('22', 12), 'hex'), decode(repeat('bb', 32), 'hex'));
  INSERT INTO privacy_purposes (
    organization_id, workspace_id, purpose_key, description, legal_basis,
    notice_reference, consent_reference, allowed_classes
  ) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'order_fulfillment', 'Fulfil synthetic customer orders', 'contract', 'privacy-notice:v1', '', '[\"personal\",\"sensitive_operational\"]'::jsonb),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'order_fulfillment', 'Fulfil synthetic customer orders', 'contract', 'privacy-notice:v1', '', '[\"personal\"]'::jsonb);
  INSERT INTO privacy_retention_policies (
    organization_id, workspace_id, purpose_key, data_class, retention_days, disposition, legal_hold_permitted
  ) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'order_fulfillment', 'personal', 365, 'anonymize', true),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'order_fulfillment', 'personal', 365, 'anonymize', true);
  INSERT INTO inbox_receipts (
    organization_id, workspace_id, consumer, event_id, event_type, event_fingerprint,
    first_observed_at, processed_attempt
  ) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'orders.projector.v1', 'evt_inbox_seed_a', 'commerce.orders.order_created.v1', repeat('1',64), '2026-08-09T10:00:00Z', 1),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'orders.projector.v1', 'evt_inbox_seed_b', 'commerce.orders.order_created.v1', repeat('2',64), '2026-08-09T10:00:00Z', 2);
  " </dev/null >/dev/null

# Task 082 Product Compliance seed: one independently scoped verified document/policy per tenant.
psql_exec "$database_name" --command "
  INSERT INTO compliance_documents(id,organization_id,workspace_id,document_type,number,jurisdiction,issuer,registry_source,status,issued_at,expires_at,created_at,updated_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0701','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','certificate','CERT-A','RU','Synthetic Registry','official_registry','draft','2026-01-01T00:00:00Z','2027-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0701','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','certificate','CERT-B','RU','Synthetic Registry','official_registry','draft','2026-01-01T00:00:00Z','2027-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
  UPDATE compliance_documents SET status='valid',verification_source='official_registry',verified_at='2026-08-10T01:00:00Z',version=2,updated_at='2026-08-10T01:00:00Z';
  INSERT INTO compliance_bindings(id,organization_id,workspace_id,document_id,subject_type,subject_id,active,created_at,updated_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0702','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0701','product','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101',true,'2026-08-10T01:00:00Z','2026-08-10T01:00:00Z'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0702','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0701','product','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101',true,'2026-08-10T01:00:00Z','2026-08-10T01:00:00Z');
  INSERT INTO compliance_policies(id,organization_id,workspace_id,code,jurisdiction,operation,connector_family,seller_role,requirements,effective_from,active,version,created_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0703','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','ru.publication.certificate','RU','publication','marketplace','seller',jsonb_build_array(jsonb_build_object('document_type','certificate','failure_outcome','block','verification_required',true,'min_validity_hours',72)),'2026-01-01T00:00:00Z',true,1,'2026-01-01T00:00:00Z'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0703','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','ru.publication.certificate','RU','publication','marketplace','seller',jsonb_build_array(jsonb_build_object('document_type','certificate','failure_outcome','block','verification_required',true,'min_validity_hours',72)),'2026-01-01T00:00:00Z',true,1,'2026-01-01T00:00:00Z');
" >/dev/null

unvalidated="$(query_scalar "$database_name" "
  SELECT count(*)
  FROM pg_constraint
  WHERE conname IN (
    'organizations_id_sortable_chk',
    'organizations_name_chk',
    'organizations_status_chk',
    'organizations_version_chk',
    'organizations_timestamps_chk',
    'workspaces_id_sortable_chk',
    'workspaces_name_chk',
    'workspaces_status_chk',
    'workspaces_version_chk',
    'workspaces_timestamps_chk',
    'connector_accounts_workspace_scope_fk',
    'outbox_events_workspace_scope_fk',
    'audit_records_workspace_scope_fk'
  ) AND NOT convalidated;
")"
[[ "$unvalidated" == 0 ]] || die "$unvalidated tenancy constraints remain unvalidated"

rls_tables="$(query_scalar "$database_name" "
  SELECT count(*)
  FROM pg_class
  WHERE relname IN ('organizations', 'workspaces', 'stores', 'connector_accounts', 'outbox_events', 'audit_records', 'secret_references', 'secret_versions', 'privacy_purposes', 'privacy_retention_policies', 'user_profiles', 'inbox_receipts', 'products', 'offers', 'connector_entity_mappings', 'prices', 'warehouses', 'inventory_positions', 'orders', 'order_items', 'lineage_records', 'lineage_inputs', 'pim_brands', 'pim_categories', 'pim_attributes', 'pim_product_brands', 'pim_product_categories', 'pim_product_attribute_values', 'pim_field_authorities', 'pim_duplicate_candidates', 'pim_merge_previews', 'legal_entities', 'individual_entrepreneurs', 'legal_branches', 'counterparties', 'legal_addresses', 'counterparty_bank_accounts', 'counterparty_contracts', 'counterparty_authorities', 'legal_party_duplicate_candidates', 'legal_party_merge_previews', 'compliance_documents', 'compliance_bindings', 'compliance_policies', 'compliance_verifications')
    AND relrowsecurity AND relforcerowsecurity;
")"
[[ "$rls_tables" == 45 ]] || die "expected forced RLS on forty-five foundation/core/PIM/profile/compliance tables, found $rls_tables"

tenant_policies="$(query_scalar "$database_name" "
  SELECT count(*)
  FROM pg_policies
  WHERE schemaname = 'public'
    AND tablename IN ('organizations', 'workspaces', 'stores', 'connector_accounts', 'outbox_events', 'audit_records', 'secret_references', 'secret_versions', 'privacy_purposes', 'privacy_retention_policies', 'user_profiles', 'inbox_receipts', 'products', 'offers', 'connector_entity_mappings', 'prices', 'warehouses', 'inventory_positions', 'orders', 'order_items', 'lineage_records', 'lineage_inputs', 'pim_brands', 'pim_categories', 'pim_attributes', 'pim_product_brands', 'pim_product_categories', 'pim_product_attribute_values', 'pim_field_authorities', 'pim_duplicate_candidates', 'pim_merge_previews', 'legal_entities', 'individual_entrepreneurs', 'legal_branches', 'counterparties', 'legal_addresses', 'counterparty_bank_accounts', 'counterparty_contracts', 'counterparty_authorities', 'legal_party_duplicate_candidates', 'legal_party_merge_previews', 'compliance_documents', 'compliance_bindings', 'compliance_policies', 'compliance_verifications');
")"
[[ "$tenant_policies" == 85 ]] || die "expected eighty-five tenant policies including profile/product-compliance policies, found $tenant_policies"
inbox_receipt_policies="$(query_scalar "$database_name" "SELECT count(*) FROM pg_policies WHERE schemaname = 'public' AND tablename = 'inbox_receipts' AND cmd IN ('SELECT','INSERT');")"
[[ "$inbox_receipt_policies" == 2 ]] || die "inbox receipts do not have exactly SELECT/INSERT tenant policies"

outbox_policy_shape="$(query_scalar "$database_name" "
  SELECT count(*) FROM pg_policies
  WHERE schemaname='public' AND tablename='outbox_events' AND cmd IN ('SELECT','INSERT','UPDATE');
")"
[[ "$outbox_policy_shape" == 3 ]] || die "outbox table does not have exactly SELECT/INSERT/UPDATE tenant policies"
outbox_delete_policies="$(query_scalar "$database_name" "
  SELECT count(*) FROM pg_policies
  WHERE schemaname='public' AND tablename='outbox_events' AND cmd IN ('DELETE','ALL');
")"
[[ "$outbox_delete_policies" == 0 ]] || die "outbox table unexpectedly exposes DELETE/ALL policy"

scope_a="
  BEGIN READ ONLY;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
"
scope_b="
  BEGIN READ ONLY;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', true);
"
same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM stores; ROLLBACK;")"
[[ "$same_tenant" == 1 ]] || die "same-tenant lookup returned $same_tenant rows"
cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM stores WHERE id = '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0003'; ROLLBACK;")"
[[ "$cross_tenant" == 0 ]] || die "cross-tenant store lookup leaked $cross_tenant rows"
profile_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM user_profiles; ROLLBACK;")"
[[ "$profile_same_tenant" == 1 ]] || die "same-tenant profile lookup returned $profile_same_tenant rows"
profile_cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM user_profiles WHERE subject_ref=repeat('b',64); ROLLBACK;")"
[[ "$profile_cross_tenant" == 0 ]] || die "cross-tenant profile lookup leaked $profile_cross_tenant rows"

mixed_scope="$(query_scalar "$database_name" "
  BEGIN READ ONLY;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', true);
  SELECT count(*) FROM stores;
  ROLLBACK;
")"
[[ "$mixed_scope" == 0 ]] || die "mixed organization/workspace scope leaked $mixed_scope rows"

inbox_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM inbox_receipts; ROLLBACK;")"
[[ "$inbox_same_tenant" == 1 ]] || die "same-tenant inbox lookup returned $inbox_same_tenant rows"
inbox_cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM inbox_receipts WHERE event_id='evt_inbox_seed_b'; ROLLBACK;")"
[[ "$inbox_cross_tenant" == 0 ]] || die "cross-tenant inbox receipt leaked"

product_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM products; ROLLBACK;")"
[[ "$product_same_tenant" == 1 ]] || die "same-tenant catalog product lookup returned $product_same_tenant rows"
product_cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM products WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101'; ROLLBACK;")"
[[ "$product_cross_tenant" == 0 ]] || die "cross-tenant catalog product leaked"
price_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM prices; ROLLBACK;")"
[[ "$price_same_tenant" == 1 ]] || die "same-tenant price lookup returned $price_same_tenant rows"
price_cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM prices WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1b0201'; ROLLBACK;")"
[[ "$price_cross_tenant" == 0 ]] || die "cross-tenant price leaked"
inventory_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM inventory_positions; ROLLBACK;")"
[[ "$inventory_same_tenant" == 1 ]] || die "same-tenant inventory lookup returned $inventory_same_tenant rows"
inventory_cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM inventory_positions WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1b0301'; ROLLBACK;")"
[[ "$inventory_cross_tenant" == 0 ]] || die "cross-tenant inventory leaked"
mapping_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM connector_entity_mappings; ROLLBACK;")"
[[ "$mapping_same_tenant" == 2 ]] || die "same-tenant connector mapping lookup returned $mapping_same_tenant rows"
mapping_cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM connector_entity_mappings WHERE remote_id='remote-product-b'; ROLLBACK;")"
[[ "$mapping_cross_tenant" == 0 ]] || die "cross-tenant connector mapping leaked"

# Task 004 lifecycle and exact domain/outbox transaction behavior.
if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  UPDATE offers SET status='active', version=version+1, updated_at=clock_timestamp() WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102';
  COMMIT;
" >/dev/null 2>&1; then
  die "draft-product offer activated unexpectedly"
fi
psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  UPDATE products SET status='active', version=version+1, updated_at=clock_timestamp() WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101';
  UPDATE offers SET status='active', version=version+1, updated_at=clock_timestamp() WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102';
  COMMIT;
" >/dev/null
if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  UPDATE products SET status='archived', version=version+1, updated_at=clock_timestamp() WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101';
  COMMIT;
" >/dev/null 2>&1; then
  die "product archived while active offer remained"
fi
if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO offers (id,organization_id,workspace_id,product_id,sku,gtin) VALUES ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0199','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101','BAD-GTIN','4006381333932');
  COMMIT;
" >/dev/null 2>&1; then
  die "invalid GTIN check digit unexpectedly persisted"
fi
psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO products (id,organization_id,workspace_id,code,title) VALUES ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0198','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','ROLLBACK-P','Rollback Product');
  INSERT INTO outbox_events (id,organization_id,workspace_id,event_type,aggregate_type,aggregate_id,payload,event_envelope) VALUES (
    'evt_catalog_rollback','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','commerce.catalog.product_changed.v1','product','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0198',
    jsonb_build_object('product_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0198','version',1,'status','draft','change','created'),
    jsonb_build_object('event_id','evt_catalog_rollback','event_type','commerce.catalog.product_changed.v1','occurred_at','2026-08-09T10:00:00Z','organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','correlation_id',NULL,'causation_id',NULL,'entity_type','product','entity_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0198','source','test','data',jsonb_build_object('product_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0198','version',1,'status','draft','change','created'))
  );
  ROLLBACK;
" >/dev/null
catalog_rollback="$(query_scalar "$database_name" "SELECT (SELECT count(*) FROM products WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0198') + (SELECT count(*) FROM outbox_events WHERE id='evt_catalog_rollback');")"
[[ "$catalog_rollback" == 0 ]] || die "catalog product/outbox rollback was not atomic"

if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  UPDATE inbox_receipts SET processed_attempt=9 WHERE event_id='evt_inbox_seed_a';
  COMMIT;
" >/dev/null 2>&1; then
  die "application role mutated immutable inbox receipt"
fi



# Task 008: domain mutation + outbox intent share one transaction. Rollback must
# remove both rows, and RLS/body immutability must fail closed.
psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO stores (id, organization_id, workspace_id, code, name) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0098', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'outbox-rollback', 'Outbox Rollback');
  INSERT INTO outbox_events (
    id, organization_id, workspace_id, event_type, aggregate_type, aggregate_id,
    payload, event_envelope
  ) VALUES (
    'evt_outbox_rollback', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',
    'commerce.orders.order_created.v1', 'order', 'order_rollback',
    jsonb_build_object('order_id','order_rollback'),
    jsonb_build_object('event_id','evt_outbox_rollback','event_type','commerce.orders.order_created.v1','occurred_at','2026-08-09T09:00:00Z','organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','correlation_id',NULL,'causation_id',NULL,'entity_type','order','entity_id','order_rollback','source','test','data',jsonb_build_object('order_id','order_rollback'))
  );
  ROLLBACK;
" >/dev/null
rollback_pair="$(query_scalar "$database_name" "SELECT (SELECT count(*) FROM stores WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0098') + (SELECT count(*) FROM outbox_events WHERE id='evt_outbox_rollback');")"
[[ "$rollback_pair" == 0 ]] || die "domain/outbox rollback was not atomic"

psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO outbox_events (
    id, organization_id, workspace_id, event_type, aggregate_type, aggregate_id,
    payload, event_envelope
  ) VALUES (
    'evt_outbox_a', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',
    'commerce.orders.order_created.v1', 'order', 'order_a',
    jsonb_build_object('order_id','order_a'),
    jsonb_build_object('event_id','evt_outbox_a','event_type','commerce.orders.order_created.v1','occurred_at','2026-08-09T09:00:00Z','organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','correlation_id',NULL,'causation_id',NULL,'entity_type','order','entity_id','order_a','source','test','data',jsonb_build_object('order_id','order_a'))
  );
  COMMIT;
" >/dev/null
outbox_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM outbox_events WHERE id='evt_outbox_a'; ROLLBACK;")"
[[ "$outbox_same_tenant" == 1 ]] || die "same-tenant outbox lookup returned $outbox_same_tenant rows"

# Task 023: canonical PIM/MDM rows and external taxonomy mappings remain tenant isolated.
pim_a="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM pim_brands; ROLLBACK;")"
[[ "$pim_a" == 1 ]] || die "same-tenant PIM brand lookup returned $pim_a rows"
pim_b_leak="$(query_scalar "$database_name" "$scope_b SELECT count(*) FROM pim_brands WHERE organization_id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001'; ROLLBACK;")"
[[ "$pim_b_leak" == 0 ]] || die "cross-tenant PIM brand lookup leaked $pim_b_leak rows"
pim_category_mapping="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM connector_entity_mappings WHERE entity_type='category'; ROLLBACK;")"
[[ "$pim_category_mapping" == 1 ]] || die "canonical category mapping not visible in tenant A"
legal_a="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM legal_entities; ROLLBACK;")"
[[ "$legal_a" == 1 ]] || die "same-tenant legal entity lookup returned $legal_a rows"
legal_b_leak="$(query_scalar "$database_name" "$scope_b SELECT count(*) FROM legal_entities WHERE organization_id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001'; ROLLBACK;")"
[[ "$legal_b_leak" == 0 ]] || die "cross-tenant legal entity lookup leaked $legal_b_leak rows"
cp_a="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM counterparties; ROLLBACK;")"
[[ "$cp_a" == 1 ]] || die "same-tenant counterparty lookup returned $cp_a rows"
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); INSERT INTO legal_entities(id,organization_id,workspace_id,code,legal_name,country_code,inn,kpp,ogrn,created_at,updated_at) VALUES('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0599','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','BAD-RU-ID','Bad RU ID','RU','7701234561','770101001','1027701234560',clock_timestamp(),clock_timestamp()); COMMIT;" >/dev/null 2>&1; then die "invalid Russian INN passed database checksum validation"; fi
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); INSERT INTO counterparties(id,organization_id,workspace_id,code,party_type,party_id,role,created_at,updated_at) VALUES('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0598','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','CROSS-TENANT','legal_entity','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0501','supplier',clock_timestamp(),clock_timestamp()); COMMIT;" >/dev/null 2>&1; then die "counterparty accepted cross-tenant legal party reference"; fi
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); DELETE FROM legal_entities WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0501'; COMMIT;" >/dev/null 2>&1; then die "legal entity hard delete was allowed"; fi
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); INSERT INTO pim_product_attribute_values(organization_id,workspace_id,product_id,attribute_id,ordinal,value,source) VALUES('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0403',1,'12.5'::jsonb,'bad.float'); COMMIT;" >/dev/null 2>&1; then die "decimal PIM attribute accepted JSON number"; fi

# Task 030: lineage is append-only, tenant-scoped and must link same-tenant audit/outbox evidence.
psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO lineage_records (id,organization_id,workspace_id,source,actor_id,operation,output_system,output_entity_type,output_entity_id,output_entity_version,output_field,transform_kind,transform_id,transform_version,correlation_id,audit_id,event_id,result,fingerprint_sha256,occurred_at) VALUES (
    'lin.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','test','system','pricing.price.updated','torgnexa','price','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0201','2','amount','domain_mutation','pricing.updated','1','seed-a','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0010','evt_outbox_a','applied',repeat('a',64),'2026-08-09T10:01:00Z'
  );
  INSERT INTO lineage_inputs (organization_id,workspace_id,record_id,position,role,source_system,source_entity_type,source_entity_id,source_entity_version,source_field) VALUES (
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','lin.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,'previous','torgnexa','price','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0201','1','amount'
  );
  COMMIT;
" >/dev/null
lineage_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM lineage_records; ROLLBACK;")"
[[ "$lineage_same_tenant" == 1 ]] || die "same-tenant lineage lookup returned $lineage_same_tenant rows"
lineage_cross_tenant="$(query_scalar "$database_name" "$scope_b SELECT count(*) FROM lineage_records; ROLLBACK;")"
[[ "$lineage_cross_tenant" == 0 ]] || die "cross-tenant lineage leaked $lineage_cross_tenant rows"
if psql_exec "$database_name" --command "
  BEGIN; SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001',true);
  SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002',true);
  INSERT INTO lineage_records (id,organization_id,workspace_id,source,operation,output_system,output_entity_type,output_entity_id,transform_kind,transform_id,transform_version,correlation_id,audit_id,event_id,result,fingerprint_sha256,occurred_at) VALUES (
    'lin.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','test','pricing.price.updated','torgnexa','price','x','domain_mutation','pricing.updated','1','seed-b','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0010','evt_outbox_a','applied',repeat('b',64),'2026-08-09T10:01:00Z');
  COMMIT;
" >/dev/null 2>&1; then die "cross-tenant lineage evidence link unexpectedly succeeded"; fi
if psql_exec "$database_name" --command "
  BEGIN; SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true);
  SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true);
  UPDATE lineage_records SET operation='tampered' WHERE id='lin.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'; COMMIT;
" >/dev/null 2>&1; then die "lineage evidence update unexpectedly succeeded"; fi

if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  UPDATE outbox_events SET payload=jsonb_build_object('order_id','tampered') WHERE id='evt_outbox_a';
  COMMIT;
" >/dev/null 2>&1; then
  die "outbox immutable event body update unexpectedly succeeded"
fi
if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  DELETE FROM outbox_events WHERE id='evt_outbox_a';
  COMMIT;
" >/dev/null 2>&1; then
  die "outbox DELETE unexpectedly succeeded"
fi

audit_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM audit_records; ROLLBACK;")"
[[ "$audit_same_tenant" == 1 ]] || die "same-tenant audit lookup returned $audit_same_tenant rows"
audit_cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM audit_records WHERE id = '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0010'; ROLLBACK;")"
[[ "$audit_cross_tenant" == 0 ]] || die "cross-tenant audit lookup leaked $audit_cross_tenant rows"

audit_policy_shape="$(query_scalar "$database_name" "
  SELECT count(*)
  FROM pg_policies
  WHERE schemaname='public' AND tablename='audit_records' AND cmd IN ('SELECT','INSERT');
")"
[[ "$audit_policy_shape" == 2 ]] || die "audit table does not have exactly SELECT/INSERT RLS policies"
audit_mutation_policies="$(query_scalar "$database_name" "
  SELECT count(*)
  FROM pg_policies
  WHERE schemaname='public' AND tablename='audit_records' AND cmd IN ('UPDATE','DELETE','ALL');
")"
[[ "$audit_mutation_policies" == 0 ]] || die "audit table unexpectedly has mutation RLS policies"


secret_ref_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM secret_references; ROLLBACK;")"
[[ "$secret_ref_same_tenant" == 1 ]] || die "same-tenant secret reference lookup returned $secret_ref_same_tenant rows"
secret_ref_cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM secret_references WHERE reference='sec:v1:22222222222222222222222222222222'; ROLLBACK;")"
[[ "$secret_ref_cross_tenant" == 0 ]] || die "cross-tenant secret reference leaked $secret_ref_cross_tenant rows"
secret_version_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM secret_versions; ROLLBACK;")"
[[ "$secret_version_same_tenant" == 1 ]] || die "same-tenant secret version lookup returned $secret_version_same_tenant rows"

secret_policy_shape="$(query_scalar "$database_name" "
  SELECT count(*) FROM pg_policies
  WHERE schemaname='public' AND (
    (tablename='secret_references' AND cmd IN ('SELECT','INSERT','UPDATE')) OR
    (tablename='secret_versions' AND cmd IN ('SELECT','INSERT'))
  );
")"
[[ "$secret_policy_shape" == 5 ]] || die "secret tables do not have expected SELECT/INSERT/UPDATE policy shape"
secret_version_mutation_policies="$(query_scalar "$database_name" "SELECT count(*) FROM pg_policies WHERE schemaname='public' AND tablename='secret_versions' AND cmd IN ('UPDATE','DELETE','ALL');")"
[[ "$secret_version_mutation_policies" == 0 ]] || die "secret_versions unexpectedly has mutation RLS policies"

plaintext_secret_columns="$(query_scalar "$database_name" "
  SELECT count(*) FROM information_schema.columns
  WHERE table_schema='public' AND table_name IN ('secret_references','secret_versions')
    AND column_name IN ('password','token','access_token','refresh_token','client_secret','secret_value','master_key');
")"
[[ "$plaintext_secret_columns" == 0 ]] || die "plaintext/master-key secret columns exist in PostgreSQL"
connector_secret_column="$(query_scalar "$database_name" "SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='connector_accounts' AND column_name='secret_reference';")"
[[ "$connector_secret_column" == 1 ]] || die "connector_accounts.secret_reference is missing"
connector_secret_fk="$(query_scalar "$database_name" "SELECT count(*) FROM pg_constraint WHERE conname='connector_accounts_secret_reference_fk' AND convalidated;")"
[[ "$connector_secret_fk" == 1 ]] || die "tenant-bound connector secret reference FK is missing"

privacy_purpose_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM privacy_purposes; ROLLBACK;")"
[[ "$privacy_purpose_same_tenant" == 1 ]] || die "same-tenant privacy purpose lookup returned $privacy_purpose_same_tenant rows"
privacy_purpose_cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM privacy_purposes WHERE organization_id='018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001'; ROLLBACK;")"
[[ "$privacy_purpose_cross_tenant" == 0 ]] || die "cross-tenant privacy purpose leaked $privacy_purpose_cross_tenant rows"
privacy_retention_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM privacy_retention_policies; ROLLBACK;")"
[[ "$privacy_retention_same_tenant" == 1 ]] || die "same-tenant retention lookup returned $privacy_retention_same_tenant rows"
privacy_policy_shape="$(query_scalar "$database_name" "
  SELECT count(*) FROM pg_policies
  WHERE schemaname='public'
    AND tablename IN ('privacy_purposes','privacy_retention_policies')
    AND cmd IN ('SELECT','INSERT','UPDATE');
")"
[[ "$privacy_policy_shape" == 6 ]] || die "privacy registry does not have expected SELECT/INSERT/UPDATE RLS policy shape"
privacy_delete_policies="$(query_scalar "$database_name" "SELECT count(*) FROM pg_policies WHERE schemaname='public' AND tablename IN ('privacy_purposes','privacy_retention_policies') AND cmd IN ('DELETE','ALL');")"
[[ "$privacy_delete_policies" == 0 ]] || die "privacy registry unexpectedly exposes DELETE RLS policies"
raw_pii_columns="$(query_scalar "$database_name" "
  SELECT count(*) FROM information_schema.columns
  WHERE table_schema='public' AND table_name IN ('privacy_purposes','privacy_retention_policies')
    AND column_name IN ('customer_email','phone_number','full_name','passport_number','subject_payload','raw_pii');
")"
[[ "$raw_pii_columns" == 0 ]] || die "privacy registry contains raw subject-PII columns"

psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO audit_records (
    id, organization_id, workspace_id, actor_id, source, action,
    resource_type, resource_id, correlation_id, risk, summary
  ) VALUES (
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0011',
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',
    'oidc:user-1', 'api', 'catalog.product.update', 'product', 'product-1',
    'request-1', 'write_sensitive', '{\"changed_fields\":[\"title\"]}'::jsonb
  );
  COMMIT;
" >/dev/null

app_audit_rows="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM audit_records WHERE correlation_id='request-1'; ROLLBACK;")"
[[ "$app_audit_rows" == 1 ]] || die "same-tenant audit insert was not visible"

if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO audit_records (
    id, organization_id, workspace_id, actor_id, source, action,
    resource_type, resource_id, correlation_id, risk, summary
  ) VALUES (
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0012',
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001',
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002',
    'oidc:user-1', 'api', 'catalog.product.update', 'product', 'product-x',
    'request-x', 'write_sensitive', '{}'::jsonb
  );
  COMMIT;
" >/dev/null 2>&1; then
  die "cross-tenant audit insert unexpectedly succeeded"
fi

if psql_exec "$database_name" --command "UPDATE audit_records SET action='tampered' WHERE correlation_id='request-1';" >/dev/null 2>&1; then
  die "privileged audit UPDATE bypassed append-only trigger"
fi
if psql_exec "$database_name" --command "DELETE FROM audit_records WHERE correlation_id='request-1';" >/dev/null 2>&1; then
  die "privileged audit DELETE bypassed append-only trigger"
fi
if psql_exec "$database_name" --command "SET ROLE torgnexa_app; TRUNCATE audit_records; RESET ROLE;" >/dev/null 2>&1; then
  die "audit TRUNCATE bypassed append-only trigger"
fi


psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO secret_references (reference, organization_id, workspace_id, class, status, current_version)
  VALUES ('sec:v1:33333333333333333333333333333333', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'oauth_refresh', 'active', 1);
  INSERT INTO secret_versions (reference, organization_id, workspace_id, version, algorithm, key_id, nonce, ciphertext)
  VALUES ('sec:v1:33333333333333333333333333333333', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 1, 'aes-256-gcm', 'synthetic-k1', decode(repeat('33', 12), 'hex'), decode(repeat('cc', 32), 'hex'));
  INSERT INTO connector_accounts (id, organization_id, workspace_id, family, provider, status, secret_reference)
  VALUES ('connector-secret-rotation', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'marketplace', 'synthetic', 'disabled', 'sec:v1:33333333333333333333333333333333');
  INSERT INTO secret_versions (reference, organization_id, workspace_id, version, algorithm, key_id, nonce, ciphertext)
  VALUES ('sec:v1:33333333333333333333333333333333', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 2, 'aes-256-gcm', 'synthetic-k2', decode(repeat('44', 12), 'hex'), decode(repeat('dd', 32), 'hex'));
  UPDATE secret_references SET current_version=2, updated_at=clock_timestamp() WHERE reference='sec:v1:33333333333333333333333333333333';
  COMMIT;
" >/dev/null
rotated_secret="$(query_scalar "$database_name" "$scope_a SELECT current_version FROM secret_references WHERE reference='sec:v1:33333333333333333333333333333333'; ROLLBACK;")"
[[ "$rotated_secret" == 2 ]] || die "application secret rotation did not activate version 2"
connector_secret_ref="$(query_scalar "$database_name" "$scope_a SELECT secret_reference FROM connector_accounts WHERE id='connector-secret-rotation'; ROLLBACK;")"
[[ "$connector_secret_ref" == 'sec:v1:33333333333333333333333333333333' ]] || die "connector account secret reference changed during rotation"

if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO connector_accounts (id, organization_id, workspace_id, family, provider, status, secret_reference)
  VALUES ('connector-cross-secret', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', 'marketplace', 'synthetic', 'disabled', 'sec:v1:22222222222222222222222222222222');
  COMMIT;
" >/dev/null 2>&1; then
  die "connector account accepted a cross-tenant secret reference"
fi

if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO secret_references (reference, organization_id, workspace_id, class, status, current_version)
  VALUES ('sec:v1:44444444444444444444444444444444', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002', 'connector_token', 'active', 1);
  COMMIT;
" >/dev/null 2>&1; then
  die "cross-tenant secret reference insert unexpectedly succeeded"
fi

if psql_exec "$database_name" --command "UPDATE secret_references SET class='erp_credential' WHERE reference='sec:v1:11111111111111111111111111111111';" >/dev/null 2>&1; then
  die "secret reference identity/class mutation bypassed guard"
fi
if psql_exec "$database_name" --command "UPDATE secret_versions SET key_id='tampered' WHERE reference='sec:v1:11111111111111111111111111111111';" >/dev/null 2>&1; then
  die "secret ciphertext UPDATE bypassed immutable trigger"
fi
if psql_exec "$database_name" --command "DELETE FROM secret_versions WHERE reference='sec:v1:11111111111111111111111111111111';" >/dev/null 2>&1; then
  die "secret ciphertext DELETE bypassed immutable trigger"
fi
if psql_exec "$database_name" --command "TRUNCATE secret_versions;" >/dev/null 2>&1; then
  die "secret ciphertext TRUNCATE bypassed immutable trigger"
fi

psql_exec "$database_name" --command "UPDATE secret_references SET status='revoked', revoked_at=statement_timestamp(), updated_at=statement_timestamp() WHERE reference='sec:v1:11111111111111111111111111111111';" >/dev/null
if psql_exec "$database_name" --command "UPDATE secret_references SET status='active', revoked_at=NULL, updated_at=clock_timestamp() WHERE reference='sec:v1:11111111111111111111111111111111';" >/dev/null 2>&1; then
  die "revoked secret reference was reactivated"
fi


psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO privacy_purposes (
    organization_id, workspace_id, purpose_key, description, legal_basis,
    notice_reference, consent_reference, allowed_classes
  ) VALUES (
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',
    'support_case', 'Support synthetic customer cases', 'legitimate_interest',
    'privacy-notice:v1', '', jsonb_build_array('personal')
  );
  INSERT INTO privacy_retention_policies (
    organization_id, workspace_id, purpose_key, data_class, retention_days, disposition, legal_hold_permitted
  ) VALUES (
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',
    'support_case', 'personal', 180, 'delete', true
  );
  COMMIT;
" >/dev/null

if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO privacy_purposes (
    organization_id, workspace_id, purpose_key, description, legal_basis,
    notice_reference, consent_reference, allowed_classes
  ) VALUES (
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002',
    'cross_tenant', 'Cross tenant synthetic purpose', 'contract',
    'privacy-notice:v1', '', jsonb_build_array('personal')
  );
  COMMIT;
" >/dev/null 2>&1; then
  die "cross-tenant privacy purpose insert unexpectedly succeeded"
fi

if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO privacy_retention_policies (
    organization_id, workspace_id, purpose_key, data_class, retention_days, disposition
  ) VALUES (
    '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',
    'support_case', 'secret', 30, 'delete'
  );
  COMMIT;
" >/dev/null 2>&1; then
  die "retention policy accepted a class not allowed by its privacy purpose"
fi

if psql_exec "$database_name" --command "UPDATE privacy_purposes SET description='tampered' WHERE purpose_key='support_case';" >/dev/null 2>&1; then
  die "privacy purpose update bypassed monotonic version guard"
fi
if psql_exec "$database_name" --command "UPDATE privacy_purposes SET allowed_classes=jsonb_build_array('sensitive_operational'), version=version+1, updated_at=clock_timestamp() WHERE purpose_key='support_case';" >/dev/null 2>&1; then
  die "privacy purpose removed a class still referenced by active retention metadata"
fi
if psql_exec "$database_name" --command "DELETE FROM privacy_purposes WHERE purpose_key='support_case';" >/dev/null 2>&1; then
  die "privacy purpose hard-delete bypassed registry guard"
fi
if psql_exec "$database_name" --command "TRUNCATE privacy_retention_policies;" >/dev/null 2>&1; then
  die "privacy retention truncate bypassed registry guard"
fi

psql_exec "$database_name" --command "UPDATE privacy_purposes SET status='retired', version=version+1, updated_at=clock_timestamp() WHERE purpose_key='support_case';" >/dev/null
if psql_exec "$database_name" --command "UPDATE privacy_purposes SET status='active', version=version+1, updated_at=clock_timestamp() WHERE purpose_key='support_case';" >/dev/null 2>&1; then
  die "retired privacy purpose was reactivated"
fi


# Task 006 normalized Orders: tenant isolation, immutable totals/lifecycle and generic remote mapping.
psql_exec "$database_name" --command "
  UPDATE products SET status='active', version=version+1, updated_at=clock_timestamp() WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101';
  UPDATE offers SET status='active', version=version+1, updated_at=clock_timestamp() WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1b0102';
  INSERT INTO orders(id,organization_id,workspace_id,order_number,currency,subtotal_minor_units,discount_minor_units,tax_minor_units,shipping_minor_units,grand_minor_units,placed_at) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0601','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','ORDER-A','RUB',1000,100,180,100,1180,'2026-08-09T09:00:00Z'),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0601','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','ORDER-B','RUB',2000,0,400,0,2400,'2026-08-09T09:00:00Z');
  INSERT INTO order_items(id,organization_id,workspace_id,order_id,position,offer_id,sku_snapshot,quantity_coefficient,quantity_scale,unit,unit_price_minor_units,subtotal_minor_units,discount_minor_units,tax_minor_units,line_total_minor_units,tax_jurisdiction,tax_category,tax_rate_coefficient,tax_rate_scale,price_includes_tax) VALUES
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0602','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0601',1,'018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102','SYNTH-A-1',1,0,'EA',1000,1000,100,180,1080,'RU','standard',2,1,false),
    ('018f0e8b-8a58-7f42-8c2d-5c2f9b1b0602','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0601',1,'018f0e8b-8a58-7f42-8c2d-5c2f9b1b0102','SYNTH-B-1',2,0,'EA',1000,2000,0,400,2400,'RU','standard',2,1,false);
" >/dev/null
order_same_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM orders; ROLLBACK;")"
[[ "$order_same_tenant" == 1 ]] || die "same-tenant order lookup returned $order_same_tenant rows"
order_cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM orders WHERE order_number='ORDER-B'; ROLLBACK;")"
[[ "$order_cross_tenant" == 0 ]] || die "cross-tenant order leaked"
order_item_cross_tenant="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM order_items WHERE order_id='018f0e8b-8a58-7f42-8c2d-5c2f9b1b0601'; ROLLBACK;")"
[[ "$order_item_cross_tenant" == 0 ]] || die "cross-tenant order item leaked"
psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); INSERT INTO connector_entity_mappings(organization_id,workspace_id,connector_account_id,entity_type,local_entity_id,remote_id) VALUES('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','connector-map-a','order','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0601','remote-order-a'); COMMIT;" >/dev/null
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); UPDATE orders SET status='fulfilled',version=version+1,updated_at=clock_timestamp() WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0601'; COMMIT;" >/dev/null 2>&1; then die "invalid pending-to-fulfilled transition succeeded"; fi
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); UPDATE orders SET grand_minor_units=9999,version=version+1,updated_at=clock_timestamp() WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0601'; COMMIT;" >/dev/null 2>&1; then die "order commercial snapshot mutation succeeded"; fi
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); UPDATE order_items SET line_total_minor_units=999 WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0602'; COMMIT;" >/dev/null 2>&1; then die "immutable order item update succeeded"; fi


# Task 010 Connector SDK: normalized account lifecycle/health, family vocabulary,
# secret-reference-only credentials and immutable history.
connector_sdk_version="$(query_scalar "$database_name" "$scope_a SELECT version FROM connector_accounts WHERE id='connector-map-a'; ROLLBACK;")"
[[ "$connector_sdk_version" == 1 ]] || die "connector account did not receive Task-010 version default"
connector_sdk_health="$(query_scalar "$database_name" "$scope_a SELECT health_status FROM connector_accounts WHERE id='connector-map-a'; ROLLBACK;")"
[[ "$connector_sdk_health" == unknown ]] || die "connector account did not start with unknown health"
psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); UPDATE connector_accounts SET health_status='healthy',health_reason_code=NULL,health_checked_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id='connector-map-a'; COMMIT;" >/dev/null
connector_sdk_health="$(query_scalar "$database_name" "$scope_a SELECT health_status FROM connector_accounts WHERE id='connector-map-a'; ROLLBACK;")"
[[ "$connector_sdk_health" == healthy ]] || die "normalized connector health update failed"
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); UPDATE connector_accounts SET health_status='unavailable',health_reason_code='Authorization: Bearer leaked',health_checked_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id='connector-map-a'; COMMIT;" >/dev/null 2>&1; then
  die "connector account accepted raw provider health text"
fi
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); INSERT INTO connector_accounts(id,organization_id,workspace_id,family,provider,status) VALUES('connector-invalid-family','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','vendor_x','synthetic','disabled'); COMMIT;" >/dev/null 2>&1; then
  die "connector account accepted non-canonical family"
fi
psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); INSERT INTO connector_accounts(id,organization_id,workspace_id,family,provider,status) VALUES('connector-fx-a','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','fx','reference-fx','disabled'); INSERT INTO connector_accounts(id,organization_id,workspace_id,family,provider,status) VALUES('connector-notify-a','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','notification','reference-sms','disabled'); COMMIT;" >/dev/null
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); DELETE FROM connector_accounts WHERE id='connector-fx-a'; COMMIT;" >/dev/null 2>&1; then
  die "connector account hard-delete bypassed Task-010 guard"
fi
if psql_exec "$database_name" --command "TRUNCATE connector_accounts;" >/dev/null 2>&1; then
  die "connector account truncate bypassed Task-010 guard"
fi

# Task 082 Product Compliance: RLS, reference guards, policy immutability and evidence lifecycle.
compliance_a="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM compliance_documents; ROLLBACK;")"
[[ "$compliance_a" == 1 ]] || die "same-tenant compliance document lookup returned $compliance_a rows"
compliance_leak="$(query_scalar "$database_name" "$scope_a SELECT count(*) FROM compliance_documents WHERE organization_id='018f0e8b-8a58-7f42-8c2d-5c2f9b1b0001'; ROLLBACK;")"
[[ "$compliance_leak" == 0 ]] || die "cross-tenant compliance evidence leaked"
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); INSERT INTO compliance_bindings(id,organization_id,workspace_id,document_id,subject_type,subject_id,created_at,updated_at) VALUES('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0798','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0701','product','018f0e8b-8a58-7f42-8c2d-5c2f9b1b0101',clock_timestamp(),clock_timestamp()); COMMIT;" >/dev/null 2>&1; then die "compliance binding accepted cross-tenant product"; fi
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); INSERT INTO compliance_bindings(id,organization_id,workspace_id,document_id,subject_type,subject_id,created_at,updated_at) VALUES('018f0e8b-8a58-7f42-8c2d-5c2f9b1a0797','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0701','gtin','4601234567894',clock_timestamp(),clock_timestamp()); COMMIT;" >/dev/null 2>&1; then die "compliance binding accepted invalid GTIN checksum"; fi
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); DELETE FROM compliance_documents WHERE id='018f0e8b-8a58-7f42-8c2d-5c2f9b1a0701'; COMMIT;" >/dev/null 2>&1; then die "compliance document hard delete was allowed"; fi
if psql_exec "$database_name" --command "BEGIN; SET LOCAL ROLE torgnexa_app; SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',true); SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002',true); UPDATE compliance_policies SET active=false WHERE code='ru.publication.certificate'; COMMIT;" >/dev/null 2>&1; then die "append-only compliance policy was mutable"; fi

reset_scope="$(query_scalar "$database_name" "
  BEGIN;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  COMMIT;
  SET ROLE torgnexa_app;
  SELECT count(*) FROM stores;
  RESET ROLE;
")"
[[ "$reset_scope" == 0 ]] || die "transaction-local tenant scope leaked after commit"

legacy_inbox_absent="$(query_scalar "$database_name" "SELECT (to_regclass('public.inbox_events') IS NULL)::text;")"
[[ "$legacy_inbox_absent" == true ]] || die "legacy tenantless inbox_events table was not retired"

if psql_exec "$database_name" --command "
  BEGIN;
  SET LOCAL ROLE torgnexa_app;
  SELECT set_config('app.organization_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001', true);
  SELECT set_config('app.workspace_id', '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002', true);
  INSERT INTO stores (id, organization_id, workspace_id, code, name)
  VALUES ('018f0e8b-8a58-7f42-8c2d-5c2f9b1a9998',
          '018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001',
          '018f0e8b-8a58-7f42-8c2d-5c2f9b1b0002',
          'cross-tenant', 'Cross Tenant');
  COMMIT;
" >/dev/null 2>&1; then
  die "cross-tenant insert unexpectedly succeeded"
fi

psql_exec postgres --command "CREATE DATABASE torgnexa_invalid;" >/dev/null
apply_migration torgnexa_invalid "$repo_root/migrations/000001_platform.sql"
psql_exec torgnexa_invalid --command "INSERT INTO organizations (id, name) VALUES ('legacy-invalid-id', 'Synthetic Legacy Row');" >/dev/null
if apply_migration torgnexa_invalid "$repo_root/migrations/000002_tenancy.sql" >/dev/null 2>&1; then
  die "migration accepted an invalid legacy identifier"
fi
stores_rolled_back="$(query_scalar torgnexa_invalid "SELECT (to_regclass('public.stores') IS NULL)::text;")"
[[ "$stores_rolled_back" == true || "$stores_rolled_back" == t ]] || die "failed migration did not roll back stores table"
status_columns="$(query_scalar torgnexa_invalid "SELECT count(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'organizations' AND column_name = 'status';")"
[[ "$status_columns" == 0 ]] || die "failed migration left partial organization columns"

echo "PostgreSQL tenancy/audit/secrets/privacy/inbox/catalog/price/inventory/orders/connector-sdk migration, two-tenant RLS, immutable commerce snapshots, normalized connector accounts, and event correctness smoke passed"
