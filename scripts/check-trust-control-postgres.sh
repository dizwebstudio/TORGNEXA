#!/usr/bin/env bash
set -euo pipefail

umask 077
export LC_ALL=C TZ=UTC

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
inventory="$repo_root/supply-chain/release-artifacts.json"
container_name="torgnexa-trust-control-smoke-${BASHPID}"
owner_password=synthetic-owner-only
app_password=synthetic-app-only
started=false

die(){ echo "check-trust-control-postgres: $*" >&2; exit 1; }
for executable in docker jq; do command -v "$executable" >/dev/null 2>&1 || die "$executable is required"; done
[[ "$container_name" =~ ^torgnexa-trust-control-smoke-[0-9]+$ ]] || die "unsafe container name"
image="$(jq -er '.development_runtime[] | select(.name == "postgres") | .image' "$inventory")" || die "PostgreSQL image is missing"
[[ "$image" =~ ^postgres:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}$ ]] || die "PostgreSQL image is not immutable"

cleanup(){ if [[ "$started" == true ]]; then docker stop --time 5 "$container_name" >/dev/null; fi; }
trap cleanup EXIT HUP INT TERM

docker run --rm --detach --name "$container_name" --network none --memory 512m --cpus 1 --pids-limit 256 \
  --env POSTGRES_DB=torgnexa --env POSTGRES_USER=torgnexa --env "POSTGRES_PASSWORD=$owner_password" \
  --volume "$repo_root/migrations:/migrations:ro" --volume "$repo_root/deploy/postgres:/deploy/postgres:ro" "$image" >/dev/null
started=true

ready=false
for _ in {1..30}; do
  if docker exec --env "PGPASSWORD=$owner_password" "$container_name" pg_isready --username torgnexa --dbname torgnexa >/dev/null 2>&1; then ready=true; break; fi
  sleep 1
done
[[ "$ready" == true ]] || die "temporary PostgreSQL did not become ready"

owner_exec(){
  docker exec --env PGHOST=127.0.0.1 --env PGPORT=5432 --env PGDATABASE=torgnexa --env PGUSER=torgnexa --env "PGPASSWORD=$owner_password" "$container_name" "$@"
}
owner_psql(){ owner_exec psql --no-psqlrc --set ON_ERROR_STOP=1 --tuples-only --no-align --quiet "$@"; }

owner_exec env TORGNEXA_VERSION=0.21.0 /bin/sh /deploy/postgres/migrate.sh >/dev/null
owner_exec env "TORGNEXA_APP_DB_PASSWORD=$app_password" /bin/sh /deploy/postgres/configure-app-role.sh >/dev/null

[[ "$(owner_psql --command 'SELECT max(version) FROM migration_history;')" == 16 ]] || die "migration 16 is not current"
[[ "$(owner_psql --command "SELECT rolsuper||':'||rolbypassrls||':'||rolcreaterole||':'||rolcreatedb FROM pg_roles WHERE rolname='torgnexa_app';")" == "false:false:false:false" ]] || die "application role is privileged"
[[ "$(owner_psql --command "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_roles r ON r.oid=c.relowner WHERE r.rolname='torgnexa_app' AND n.nspname NOT IN ('pg_catalog','information_schema');")" == 0 ]] || die "application role owns runtime state"
[[ "$(owner_psql --command "SELECT count(*) FROM pg_class WHERE relname IN ('operation_receipts','security_evidence','mcp_credential_activity','ai_egress_policy_revisions','ai_egress_usage','connector_replay_runs','profitability_scenarios') AND relrowsecurity AND relforcerowsecurity;")" == 7 ]] || die "trust-control forced RLS is incomplete"

owner_psql --command "
  INSERT INTO organizations(id,name) VALUES('018f0e8b-8a58-7f42-8c2d-5c2f9b1c0001','Synthetic Trust Organization');
  INSERT INTO workspaces(id,organization_id,name) VALUES('018f0e8b-8a58-7f42-8c2d-5c2f9b1c0002','018f0e8b-8a58-7f42-8c2d-5c2f9b1c0001','Synthetic Trust Workspace');
" >/dev/null

docker exec --env PGHOST=127.0.0.1 --env PGPORT=5432 --env PGDATABASE=torgnexa --env PGUSER=torgnexa_app --env "PGPASSWORD=$app_password" "$container_name" \
  psql --no-psqlrc --set ON_ERROR_STOP=1 --quiet --command "
    BEGIN;
    SELECT set_config('app.organization_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1c0001',true);
    SELECT set_config('app.workspace_id','018f0e8b-8a58-7f42-8c2d-5c2f9b1c0002',true);
    SELECT pg_advisory_xact_lock(hashtextextended('ai-egress:smoke',0));
    INSERT INTO operation_receipts(organization_id,workspace_id,operation,idempotency_key,request_sha256,state) VALUES('018f0e8b-8a58-7f42-8c2d-5c2f9b1c0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1c0002','trust.smoke','smoke-key',decode(repeat('ab',32),'hex'),'pending');
    UPDATE operation_receipts SET state='completed',resource_type='smoke',resource_id='smoke-1',result='{\"ok\":true}'::jsonb,completed_at=clock_timestamp() WHERE operation='trust.smoke' AND idempotency_key='smoke-key';
    INSERT INTO security_evidence(id,organization_id,workspace_id,evidence_type,actor_ref,resource_type,resource_id,correlation_id,decision,summary) VALUES('smoke-evidence','018f0e8b-8a58-7f42-8c2d-5c2f9b1c0001','018f0e8b-8a58-7f42-8c2d-5c2f9b1c0002','trust.smoke','synthetic-actor','smoke','smoke-1','smoke-key','succeeded','{}'::jsonb);
    COMMIT;
  " >/dev/null

if docker exec --env PGHOST=127.0.0.1 --env PGPORT=5432 --env PGDATABASE=torgnexa --env PGUSER=torgnexa_app --env "PGPASSWORD=$app_password" "$container_name" psql --no-psqlrc --quiet --command 'CREATE TABLE public.should_not_exist(id integer);' >/dev/null 2>&1; then
  die "application role can create public schema objects"
fi
if docker exec --env PGHOST=127.0.0.1 --env PGPORT=5432 --env PGDATABASE=torgnexa --env PGUSER=torgnexa_app --env "PGPASSWORD=$app_password" "$container_name" psql --no-psqlrc --quiet --command "UPDATE security_evidence SET decision='failed';" >/dev/null 2>&1; then
  die "append-only evidence can be updated"
fi

echo "trust-control PostgreSQL migration/role/RLS: PASS"
