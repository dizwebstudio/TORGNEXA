#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"

for command_name in node curl; do
  command -v "$command_name" >/dev/null || { echo "community-e2e: требуется команда $command_name" >&2; exit 1; }
done

frontend_url="${TORGNEXA_E2E_BASE_URL:-http://127.0.0.1:5173}"
if ! curl --silent --show-error --fail --max-time 3 "$frontend_url/" >/dev/null 2>&1; then
  echo 'community-e2e: frontend недоступен; сначала выполните make community-up' >&2
  exit 1
fi

./scripts/ensure-community-demo-user.sh
exec node scripts/community-e2e.mjs "$@"
