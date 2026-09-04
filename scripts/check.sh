#!/usr/bin/env bash
set -euo pipefail

if [[ -n "${CI:-}" || -n "${GITHUB_ACTIONS:-}" ]]; then
  export TORGNEXA_SANDBOX_ALLOW_CONTAINER_FALLBACK="${TORGNEXA_SANDBOX_ALLOW_CONTAINER_FALLBACK:-1}"
fi

make check
make build
