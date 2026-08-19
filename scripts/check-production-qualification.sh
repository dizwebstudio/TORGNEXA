#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
command -v docker >/dev/null 2>&1 || { echo "Docker is required for P3 production qualification" >&2; exit 1; }
docker compose version >/dev/null
command -v python3 >/dev/null 2>&1 || { echo "python3 is required for bounded load qualification" >&2; exit 1; }

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
evidence="${TORGNEXA_QUALIFICATION_EVIDENCE_DIR:-$root/qualification/evidence/$stamp}"
mkdir -p "$evidence"
project="torgnexa-p3q-$RANDOM-$$"
generated_env=0
if [[ ! -f .env ]]; then
  ./scripts/init-community-env.sh >/dev/null
  generated_env=1
fi
cleanup() {
  docker compose -p "$project" --env-file .env down -v --remove-orphans >/dev/null 2>&1 || true
  if [[ "$generated_env" == 1 ]]; then rm -f .env; fi
}
trap cleanup EXIT

dc=(docker compose -p "$project" --env-file .env)
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
qualify_runtime runtime-initial
api_binding="$(${dc[@]} port api 8080 | tail -n 1)"
api_port="${api_binding##*:}"
[[ "$api_port" =~ ^[0-9]+$ ]] || { echo "unable to resolve published API port from: $api_binding" >&2; exit 1; }
python3 scripts/runtime-load.py --url "http://127.0.0.1:$api_port/api/v1/health" --output "$evidence/api-load.json"

# Graceful application restart drill.
${dc[@]} stop -t 15 worker >/dev/null
${dc[@]} start worker >/dev/null
wait_running worker
qualify_runtime runtime-after-worker-restart

# Kafka outage/recovery drill. Worker may restart under the Compose restart policy;
# qualification resumes only after broker health and consumer progress return.
${dc[@]} restart kafka >/dev/null
wait_healthy kafka
wait_running worker
qualify_runtime runtime-after-kafka-restart

# PostgreSQL outage/recovery drill validates pool/worker recovery against the same
# durable state, then re-runs Outbox -> Kafka -> Inbox.
${dc[@]} restart postgres >/dev/null
wait_healthy postgres
wait_running worker
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
