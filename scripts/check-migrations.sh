#!/usr/bin/env bash
set -euo pipefail

export GOTELEMETRY=off
export GOTOOLCHAIN=local
export GOWORK=off
export LC_ALL=C
export TZ=UTC

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd -- "$repo_root"
go run -mod=readonly ./tools/migrationcheck --root .
./scripts/check-pre-v1-baseline.sh
