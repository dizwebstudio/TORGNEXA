#!/usr/bin/env bash
set -euo pipefail

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "connector sandbox runtime qualification: SKIP (Linux only)"
  exit 0
fi
if ! command -v unshare >/dev/null 2>&1; then
  echo "connector sandbox runtime qualification: FAIL (unshare missing)" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/scripts/connector-sandbox-runtime.sh"
if ! connector_sandbox_namespace_available; then
  connector_sandbox_run_in_container "$ROOT" "scripts/check-connector-sandbox-linux.sh"
  exit $?
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

cd "$ROOT"
CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "$TMP/torgnexa-connector-emulator" ./cmd/torgnexa-connector-emulator
TORGNEXA_EMULATOR_BINARY="$TMP/torgnexa-connector-emulator" \
  go test -count=1 -run '^TestLinuxSandboxExternalIsolationProbe$' -v ./internal/platform/connectorsandbox
