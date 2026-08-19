#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
GOTOOLCHAIN=local go test ./internal/platform/slo
GOTOOLCHAIN=local go run ./cmd/torgnexa-slo-report > "$tmp"
diff -u performance/baseline-v1.json "$tmp"
echo "SLO/performance repository baseline: PASS"
