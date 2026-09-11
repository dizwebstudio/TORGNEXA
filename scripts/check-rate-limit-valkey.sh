#!/usr/bin/env bash
set -euo pipefail
umask 077
export GOTOOLCHAIN=local GOTELEMETRY=off LC_ALL=C TZ=UTC
repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"
for required in docker jq go; do command -v "$required" >/dev/null; done

container_name="torgnexa-rate-limit-${BASHPID}"
started=false
cleanup() {
  if [[ "$started" == true ]]; then docker stop --time 5 "$container_name" >/dev/null; fi
}
trap cleanup EXIT

valkey_image="${TORGNEXA_TEST_VALKEY_IMAGE:-$(jq -er '.development_runtime[] | select(.name == "valkey") | .image' supply-chain/release-artifacts.json)}"
docker run --rm --detach --name "$container_name" --memory 256m --cpus 2 --pids-limit 128 \
  --publish 127.0.0.1::6379 "$valkey_image" valkey-server --save '' --appendonly no >/dev/null
started=true

ready=false
for _ in {1..30}; do
  if docker exec "$container_name" valkey-cli ping 2>/dev/null | grep -qx PONG; then ready=true; break; fi
  sleep 1
done
[[ "$ready" == true ]] || { echo 'Temporary Valkey unavailable' >&2; exit 1; }

export TORGNEXA_TEST_VALKEY_ADDR
TORGNEXA_TEST_VALKEY_ADDR="$(docker port "$container_name" 6379/tcp)"
go test -count=1 -race -v ./internal/platform/securityedge ./internal/app/api -run '^(TestValkey|TestRateLimitReplicasShareBudget)'
echo 'Valkey rate-limit integration: PASS'
