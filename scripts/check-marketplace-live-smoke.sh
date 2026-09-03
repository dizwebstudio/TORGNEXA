#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=local
export GOWORK=off
export TZ=UTC

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$root"

connector="${TORGNEXA_MARKETPLACE_SMOKE_CONNECTOR:-}"
output="${TORGNEXA_MARKETPLACE_SMOKE_OUTPUT:-}"
if [[ -z "$connector" ]]; then
  echo "marketplace live smoke: set TORGNEXA_MARKETPLACE_SMOKE_CONNECTOR to wildberries or ozon" >&2
  exit 2
fi
if [[ -z "$output" || "$output" != /* ]]; then
  echo "marketplace live smoke: TORGNEXA_MARKETPLACE_SMOKE_OUTPUT must be an absolute path" >&2
  exit 2
fi

case "$connector" in
  wildberries)
    if [[ -z "${TORGNEXA_MARKETPLACE_SMOKE_SECRET:-}" ]]; then
      export TORGNEXA_MARKETPLACE_SMOKE_SECRET="${TORGNEXA_MARKETPLACE_SMOKE_WB_TOKEN:-}"
    fi
    if [[ -z "${TORGNEXA_MARKETPLACE_SMOKE_WAREHOUSE_ID:-}" ]]; then
      export TORGNEXA_MARKETPLACE_SMOKE_WAREHOUSE_ID="${TORGNEXA_MARKETPLACE_SMOKE_WB_WAREHOUSE_ID:-}"
    fi
    if [[ -z "${TORGNEXA_MARKETPLACE_SMOKE_VARIANT_ID:-}" ]]; then
      export TORGNEXA_MARKETPLACE_SMOKE_VARIANT_ID="${TORGNEXA_MARKETPLACE_SMOKE_WB_VARIANT_ID:-}"
    fi
    ;;
  ozon)
    if [[ -z "${TORGNEXA_MARKETPLACE_SMOKE_SECRET:-}" ]]; then
      export TORGNEXA_MARKETPLACE_SMOKE_SECRET="${TORGNEXA_MARKETPLACE_SMOKE_OZON_CLIENT_ID:-}"$'\n'"${TORGNEXA_MARKETPLACE_SMOKE_OZON_API_KEY:-}"
    fi
    if [[ -z "${TORGNEXA_MARKETPLACE_SMOKE_WAREHOUSE_ID:-}" ]]; then
      export TORGNEXA_MARKETPLACE_SMOKE_WAREHOUSE_ID="${TORGNEXA_MARKETPLACE_SMOKE_OZON_WAREHOUSE_ID:-}"
    fi
    if [[ -z "${TORGNEXA_MARKETPLACE_SMOKE_VARIANT_ID:-}" ]]; then
      export TORGNEXA_MARKETPLACE_SMOKE_VARIANT_ID="${TORGNEXA_MARKETPLACE_SMOKE_OZON_OFFER_ID:-}"
    fi
    ;;
esac

run_smoke() {
  go run ./cmd/torgnexa-marketplace-live-smoke -connector "$connector" -output "$output"
}

if command -v go >/dev/null 2>&1; then
  set +e
  run_smoke
  smoke_status=$?
  set -e
else
  command -v docker >/dev/null 2>&1 || {
    echo "marketplace live smoke: Go or Docker is required" >&2
    exit 1
  }

  output_dir="$(dirname -- "$output")"
  output_name="$(basename -- "$output")"
  runner_uid="$(id -u)"
  runner_gid="$(id -g)"
  mkdir -p "$output_dir"

  # Pass only the documented smoke variables. Docker receives their values from
  # the environment without placing them in this command's argument list.
  set +e
  docker run --rm --network host \
    --user "$runner_uid:$runner_gid" \
    -v "$root:/app" \
    -v "$output_dir:/evidence" \
    -w /app \
    -e GOCACHE=/tmp/torgnexa-go-cache \
    -e GOMODCACHE=/tmp/torgnexa-go-mod-cache \
    -e TORGNEXA_MARKETPLACE_SMOKE_CONNECTOR \
    -e TORGNEXA_MARKETPLACE_SMOKE_OUTPUT="/evidence/$output_name" \
    -e TORGNEXA_MARKETPLACE_SMOKE_ENVIRONMENT \
    -e TORGNEXA_MARKETPLACE_SMOKE_TARGET \
    -e TORGNEXA_MARKETPLACE_SMOKE_ACCOUNT_REF \
    -e TORGNEXA_MARKETPLACE_SMOKE_RELEASE_COMMIT \
    -e TORGNEXA_MARKETPLACE_SMOKE_REPOSITORY \
    -e TORGNEXA_MARKETPLACE_SMOKE_SCOPE \
    -e TORGNEXA_MARKETPLACE_SMOKE_LOCALE \
    -e TORGNEXA_MARKETPLACE_SMOKE_JURISDICTION \
    -e TORGNEXA_MARKETPLACE_SMOKE_CATEGORY_CODE \
    -e TORGNEXA_MARKETPLACE_SMOKE_SECRET \
    -e TORGNEXA_MARKETPLACE_SMOKE_WAREHOUSE_ID \
    -e TORGNEXA_MARKETPLACE_SMOKE_VARIANT_ID \
    -e TORGNEXA_MARKETPLACE_SMOKE_ALLOW_WRITES \
    -e TORGNEXA_MARKETPLACE_SMOKE_FLOW_REF \
    -e TORGNEXA_MARKETPLACE_SMOKE_ORDER_REF \
    -e TORGNEXA_MARKETPLACE_SMOKE_RESERVATION_REF \
    -e TORGNEXA_MARKETPLACE_SMOKE_SHIPMENT_REF \
    -e TORGNEXA_MARKETPLACE_SMOKE_RETURN_REF \
    -e TORGNEXA_MARKETPLACE_SMOKE_REFUND_REF \
    -e TORGNEXA_MARKETPLACE_SMOKE_SETTLEMENT_REF \
    -e TORGNEXA_MARKETPLACE_SMOKE_MARKING_REF \
    -e TORGNEXA_MARKETPLACE_SMOKE_EDO_REF \
    golang:1.26.7-alpine3.23 \
    sh -c 'export GOTOOLCHAIN=local GOWORK=off TZ=UTC; go run ./cmd/torgnexa-marketplace-live-smoke -connector "$TORGNEXA_MARKETPLACE_SMOKE_CONNECTOR" -output "$TORGNEXA_MARKETPLACE_SMOKE_OUTPUT"'
  smoke_status=$?
  set -e
fi

if [[ ! -f "$output" ]]; then
  echo "marketplace live smoke: runner did not produce evidence at $output" >&2
  exit 1
fi
PYTHONDONTWRITEBYTECODE=1 python3 "$root/scripts/marketplace_live_smoke_evidence.py" "$output"
exit "$smoke_status"
