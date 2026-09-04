#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
command -v docker >/dev/null 2>&1 || { echo "Docker is required for P3 production qualification" >&2; exit 1; }
docker compose version >/dev/null
command -v python3 >/dev/null 2>&1 || { echo "python3 is required for bounded load qualification" >&2; exit 1; }
command -v base64 >/dev/null 2>&1 || { echo "base64 is required for disposable qualification credentials" >&2; exit 1; }

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
evidence="${TORGNEXA_QUALIFICATION_EVIDENCE_DIR:-$root/qualification/evidence/$stamp}"
mkdir -p "$evidence"
project="torgnexa-p3q-$RANDOM-$$"
qualification_rate_limit="${TORGNEXA_QUALIFICATION_RATE_PER_MINUTE:-10000}"
if [[ ! "$qualification_rate_limit" =~ ^[1-9][0-9]{0,6}$ ]]; then
  echo "TORGNEXA_QUALIFICATION_RATE_PER_MINUTE must be a positive integer" >&2
  exit 1
fi
generated_env=0
if [[ ! -f .env ]]; then
  ./scripts/init-community-env.sh >/dev/null
  generated_env=1
fi

# The qualification project owns disposable, project-scoped volumes. Always
# override a stale or ambient master key with a fresh padded Base64 encoding so
# a local .env or CI environment cannot make the ephemeral stack unreadable.
# This key is never persisted or printed; production/community credentials
# remain governed by their existing .env/secret-manager lifecycle.
qualification_master_key="$(head -c 32 /dev/urandom | base64 | tr -d '\n')"
if [[ ! "$qualification_master_key" =~ ^[A-Za-z0-9+/]{43}=$ ]]; then
  echo "unable to generate a padded 32-byte qualification master key" >&2
  exit 1
fi
export TORGNEXA_SECRETS_MASTER_KEY="$qualification_master_key"
unset qualification_master_key

# The qualification stack is disposable and must be runnable alongside a
# developer or staging stack. Resolve free host ports instead of inheriting
# the shared compose defaults from .env (6379/8080/etc.). The container-side
# ports remain unchanged, so service-to-service URLs and the scenario itself
# are not affected.
pick_free_port() {
  python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
}
export POSTGRES_PORT="$(pick_free_port)"
export KAFKA_PORT="$(pick_free_port)"
export VALKEY_PORT="$(pick_free_port)"
export CLICKHOUSE_HTTP_PORT="$(pick_free_port)"
export CLICKHOUSE_NATIVE_PORT="$(pick_free_port)"
export S3_PORT="$(pick_free_port)"
export KEYCLOAK_PORT="$(pick_free_port)"
export TORGNEXA_API_PORT="$(pick_free_port)"
export TORGNEXA_MCP_PORT="$(pick_free_port)"
export TORGNEXA_FRONTEND_PORT="$(pick_free_port)"
cleanup() {
  if [[ "${TORGNEXA_QUALIFICATION_KEEP_CONTAINERS:-0}" == "1" ]]; then
    echo "qualification containers kept for inspection: $project" >&2
    return
  fi
  docker compose -p "$project" --env-file .env down -v --remove-orphans >/dev/null 2>&1 || true
  if [[ "$generated_env" == 1 ]]; then rm -f .env; fi
}
trap cleanup EXIT

# The black-box SLO probe intentionally sends a short burst of 5000 requests.
# Keep the deployment's normal edge rate limit unchanged, but give this
# disposable qualification project a bounded higher budget so the probe
# measures API availability/latency rather than the security throttle itself.
export TORGNEXA_QUALIFICATION_RATE_PER_MINUTE="$qualification_rate_limit"
dc=(docker compose -f docker-compose.yml -f docker-compose.qualification.yml -p "$project" --env-file .env)
wait_healthy() {
  local service="$1" deadline=$((SECONDS+120)) cid state
  while (( SECONDS < deadline )); do
    cid="$(${dc[@]} ps -q "$service" 2>/dev/null || true)"
    if [[ -n "$cid" ]]; then
      state="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$cid" 2>/dev/null || true)"
      [[ "$state" == healthy || "$state" == running ]] && return 0
    fi
    sleep 2
  done
  echo "service $service did not become healthy/running" >&2
  ${dc[@]} ps >&2 || true
  echo "--- $service startup logs (last 120 lines) ---" >&2
  ${dc[@]} logs --no-color --tail 120 "$service" >&2 || true
  return 1
}
wait_healthy_after_restart() {
  local service="$1" previous_started_at="$2" deadline=$((SECONDS+120)) cid state started_at
  while (( SECONDS < deadline )); do
    cid="$(${dc[@]} ps -q "$service" 2>/dev/null || true)"
    if [[ -n "$cid" ]]; then
      state="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$cid" 2>/dev/null || true)"
      started_at="$(docker inspect --format '{{.State.StartedAt}}' "$cid" 2>/dev/null || true)"
      if [[ "$started_at" != "" && "$started_at" != "$previous_started_at" && "$state" == healthy ]]; then
        return 0
      fi
    fi
    sleep 2
  done
  echo "service $service did not become healthy/running after restart" >&2
  ${dc[@]} ps >&2 || true
  echo "--- $service recovery logs (last 120 lines) ---" >&2
  ${dc[@]} logs --no-color --tail 120 "$service" >&2 || true
  return 1
}
wait_running() {
  local service="$1" deadline=$((SECONDS+120)) cid state
  while (( SECONDS < deadline )); do
    cid="$(${dc[@]} ps -q "$service" 2>/dev/null || true)"
    if [[ -n "$cid" ]]; then
      state="$(docker inspect --format '{{.State.Status}}' "$cid" 2>/dev/null || true)"
      [[ "$state" == running ]] && return 0
    fi
    sleep 2
  done
  echo "service $service did not become running" >&2
  echo "--- $service startup logs (last 120 lines) ---" >&2
  ${dc[@]} logs --no-color --tail 120 "$service" >&2 || true
  return 1
}
wait_worker_ready() {
  local deadline=$((SECONDS+120)) cid state started_at logs stable_started_at stable_deadline current_started_at
  while (( SECONDS < deadline )); do
    cid="$(${dc[@]} ps -q worker 2>/dev/null || true)"
    if [[ -n "$cid" ]]; then
      state="$(docker inspect --format '{{.State.Status}}' "$cid" 2>/dev/null || true)"
      started_at="$(docker inspect --format '{{.State.StartedAt}}' "$cid" 2>/dev/null || true)"
      if [[ "$state" == running && "$started_at" != "" ]]; then
        logs="$(${dc[@]} logs --no-color --since "$started_at" worker 2>/dev/null || true)"
        if grep -q 'worker runtime ready' <<<"$logs"; then
          # A worker can report its composition root as ready just before a
          # Kafka component observes a stale broker connection and terminates
          # the process. Require a short stable window before publishing the
          # probe event, otherwise the probe races the restart policy.
          stable_started_at="$started_at"
          stable_deadline=$((SECONDS+10))
          while (( SECONDS < stable_deadline )); do
            current_started_at="$(docker inspect --format '{{.State.StartedAt}}' "$cid" 2>/dev/null || true)"
            state="$(docker inspect --format '{{.State.Status}}' "$cid" 2>/dev/null || true)"
            if [[ "$state" != running || "$current_started_at" != "$stable_started_at" ]]; then
              break
            fi
            sleep 2
          done
          if (( SECONDS >= stable_deadline )); then
            return 0
          fi
        fi
      fi
    fi
    sleep 2
  done
  echo "worker did not report runtime readiness" >&2
  ${dc[@]} logs --no-color --tail 120 worker >&2 || true
  return 1
}
qualify_runtime() {
  local name="$1"
  ${dc[@]} run --rm --no-deps --entrypoint /app/torgnexa-runtime-qualifier worker > "$evidence/$name.json"
  python3 - "$evidence/$name.json" <<'PY'
import json,sys
v=json.load(open(sys.argv[1],encoding='utf-8'))
assert v.get('status') == 'PASS' and v.get('duplicate_receipt_count') == 1 and v.get('marker_receipt_observed') is True and v.get('warehouse_incident_observed') is True and v.get('warehouse_routed_count', 0) >= 1 and v.get('warehouse_rerouted_allocation_count', 0) >= 1 and v.get('warehouse_execution_attention_count', 0) == 0 and v.get('warehouse_source_stock_unchanged') is True and v.get('warehouse_destination_valid') is True and v.get('warehouse_allocation_rerouted') is True and v.get('warehouse_source_reservation_released') is True and v.get('warehouse_destination_reserved') is True and v.get('fulfillment_outbox_observed') is True
PY
}

./scripts/check-performance-slo.sh > "$evidence/repository-slo.txt"
${dc[@]} up -d --build api worker
wait_healthy postgres
wait_healthy kafka
wait_healthy api
wait_running worker
wait_worker_ready
qualify_runtime runtime-initial
api_binding="$(${dc[@]} port api 8080 | tail -n 1)"
api_port="${api_binding##*:}"
[[ "$api_port" =~ ^[0-9]+$ ]] || { echo "unable to resolve published API port from: $api_binding" >&2; exit 1; }
python3 scripts/runtime-load.py --url "http://127.0.0.1:$api_port/api/v1/health" --output "$evidence/api-load.json"

# Graceful application restart drill.
${dc[@]} stop -t 15 worker >/dev/null
${dc[@]} start worker >/dev/null
wait_running worker
wait_worker_ready
qualify_runtime runtime-after-worker-restart

# Kafka outage/recovery drill. Worker may restart under the Compose restart policy;
# qualification resumes only after broker health and consumer progress return.
kafka_started_at="$(${dc[@]} ps -q kafka | xargs -r docker inspect --format '{{.State.StartedAt}}')"
${dc[@]} restart kafka >/dev/null
wait_healthy_after_restart kafka "$kafka_started_at"
# Recreate the worker only after the broker is healthy.  This makes the drill
# deterministic on a single-node Compose deployment: the worker's Kafka
# clients are created against the recovered broker instead of racing its
# listener restart.  The normal `restart: unless-stopped` policy still handles
# an unplanned worker exit in production; this explicit restart validates the
# documented recovery runbook path.
${dc[@]} restart worker >/dev/null
wait_running worker
wait_worker_ready
qualify_runtime runtime-after-kafka-restart

# PostgreSQL outage/recovery drill validates pool/worker recovery against the same
# durable state, then re-runs Outbox -> Kafka -> Inbox.
postgres_started_at="$(${dc[@]} ps -q postgres | xargs -r docker inspect --format '{{.State.StartedAt}}')"
${dc[@]} restart postgres >/dev/null
wait_healthy_after_restart postgres "$postgres_started_at"
wait_running worker
wait_worker_ready
qualify_runtime runtime-after-postgres-restart

python3 - "$evidence" "$project" <<'PY'
import json, os, platform, subprocess, sys, time
out, project=sys.argv[1],sys.argv[2]
def run(*args):
    return subprocess.check_output(args,text=True,stderr=subprocess.STDOUT).strip()
meta={
  'status':'PASS',
  'qualified_at':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),
  'project':project,
  'host':{'system':platform.system(),'release':platform.release(),'machine':platform.machine(),'cpu_count':os.cpu_count()},
  'docker_version':run('docker','version','--format','{{.Server.Version}}'),
  'compose_version':run('docker','compose','version','--short'),
  'evidence_files':sorted(x for x in os.listdir(out) if x != 'metadata.json'),
}
with open(os.path.join(out,'metadata.json'),'w',encoding='utf-8') as f: json.dump(meta,f,indent=2,sort_keys=True); f.write('\n')
PY

echo "P3 production runtime qualification: PASS"
echo "evidence: $evidence"
