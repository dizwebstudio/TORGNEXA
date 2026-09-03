#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=local
export GOWORK=off
export TZ=UTC

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd -- "$root"

golden_evidence="${TORGNEXA_PRODUCTION_GOLDEN_PATH_EVIDENCE_FILE:-}"
marketplace_evidence="${TORGNEXA_MARKETPLACE_EVIDENCE_FILE:-}"
marketplace_live_smoke="${TORGNEXA_MARKETPLACE_LIVE_SMOKE_EVIDENCE_FILE:-}"
marketplace_compensation="${TORGNEXA_MARKETPLACE_COMPENSATION_EVIDENCE_FILE:-}"
carrier_evidence="${TORGNEXA_CARRIER_GOLDEN_PATH_EVIDENCE_FILE:-}"
payment_evidence="${TORGNEXA_PAYMENT_GOLDEN_PATH_EVIDENCE_FILE:-}"
fiscal_evidence="${TORGNEXA_FISCAL_GOLDEN_PATH_EVIDENCE_FILE:-}"
marking_evidence="${TORGNEXA_MARKING_GOLDEN_PATH_EVIDENCE_FILE:-}"
edo_evidence="${TORGNEXA_EDO_GOLDEN_PATH_EVIDENCE_FILE:-}"

require_absolute_file() {
  local name="$1"
  local path="$2"
  if [[ -z "$path" || "$path" != /* ]]; then
    echo "production golden path: $name must be an absolute path" >&2
    exit 2
  fi
  if [[ -L "$path" || ! -f "$path" ]]; then
    echo "production golden path: $name must be a regular non-symlink file" >&2
    exit 2
  fi
}

require_absolute_file TORGNEXA_PRODUCTION_GOLDEN_PATH_EVIDENCE_FILE "$golden_evidence"
require_absolute_file TORGNEXA_MARKETPLACE_EVIDENCE_FILE "$marketplace_evidence"
require_absolute_file TORGNEXA_MARKETPLACE_LIVE_SMOKE_EVIDENCE_FILE "$marketplace_live_smoke"
require_absolute_file TORGNEXA_MARKETPLACE_COMPENSATION_EVIDENCE_FILE "$marketplace_compensation"
require_absolute_file TORGNEXA_CARRIER_GOLDEN_PATH_EVIDENCE_FILE "$carrier_evidence"
require_absolute_file TORGNEXA_PAYMENT_GOLDEN_PATH_EVIDENCE_FILE "$payment_evidence"
require_absolute_file TORGNEXA_FISCAL_GOLDEN_PATH_EVIDENCE_FILE "$fiscal_evidence"
require_absolute_file TORGNEXA_MARKING_GOLDEN_PATH_EVIDENCE_FILE "$marking_evidence"
require_absolute_file TORGNEXA_EDO_GOLDEN_PATH_EVIDENCE_FILE "$edo_evidence"

release_commit="$(git rev-parse HEAD)"
synthetic_dir="$(mktemp -d "${TMPDIR:-/tmp}/torgnexa-production-golden-path.XXXXXX")"
trap 'rm -rf "$synthetic_dir"' EXIT

run_synthetic() {
  make order-fulfillment-qualification
}

if command -v go >/dev/null 2>&1; then
  run_synthetic >"$synthetic_dir/synthetic.log" 2>&1
else
  command -v docker >/dev/null 2>&1 || {
    echo "production golden path: Go or Docker is required for synthetic qualification" >&2
    exit 1
  }
  runner_uid="$(id -u)"
  runner_gid="$(id -g)"
  docker run --rm \
    --user "$runner_uid:$runner_gid" \
    -v "$root:/app" \
    -w /app \
    -e GOCACHE=/tmp/torgnexa-go-cache \
    -e GOMODCACHE=/tmp/torgnexa-go-mod-cache \
    golang:1.26.7-alpine3.23 \
    sh -c 'export GOTOOLCHAIN=local GOWORK=off TZ=UTC; bash ./scripts/check-order-fulfillment-qualification.sh' \
    >"$synthetic_dir/synthetic.log" 2>&1
fi

expected_repository="${TORGNEXA_P4_REPOSITORY:-}"
validator_args=(
  --input "$golden_evidence"
  --marketplace-evidence "$marketplace_evidence"
  --marketplace-live-smoke "$marketplace_live_smoke"
  --marketplace-compensation "$marketplace_compensation"
  --carrier-evidence "$carrier_evidence"
  --payment-evidence "$payment_evidence"
  --fiscal-evidence "$fiscal_evidence"
  --marking-evidence "$marking_evidence"
  --edo-evidence "$edo_evidence"
  --expected-release-commit "$release_commit"
)
if [[ -n "$expected_repository" ]]; then
  validator_args+=(--expected-repository "$expected_repository")
fi

PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$root/scripts" \
  python3 "$root/scripts/production_golden_path.py" "${validator_args[@]}"

echo "Production golden path qualification: PASS (synthetic + credentialed retained evidence)"
echo "Synthetic evidence was exercised for release commit $release_commit"
