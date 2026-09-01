#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
if command -v go >/dev/null 2>&1; then
  GOTOOLCHAIN=local go test ./internal/platform/slo
  GOTOOLCHAIN=local go run ./cmd/torgnexa-slo-report > "$tmp"
else
  command -v docker >/dev/null 2>&1 || { echo "SLO/performance qualification requires Go or Docker" >&2; exit 1; }
  qualification_image="${TORGNEXA_GO_QUALIFICATION_IMAGE:-golang:1.26-alpine}"
  docker run --rm -v "$root":/app -w /app "$qualification_image" sh -eu -c '
    export GOTOOLCHAIN=local GOWORK=off
    go test ./internal/platform/slo >&2
    go run ./cmd/torgnexa-slo-report
  ' > "$tmp"
fi
diff -u performance/baseline-v1.json "$tmp"
echo "SLO/performance repository baseline: PASS"
