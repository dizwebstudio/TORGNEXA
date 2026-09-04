#!/usr/bin/env bash

# Shared runtime qualification helpers. The container fallback is deliberately
# local-only: pull requests must use a namespace-enabled runner instead of
# executing untrusted repository code with expanded container capabilities.

readonly TORGNEXA_SANDBOX_QUALIFICATION_IMAGE='golang:1.26.7-bookworm@sha256:e8c859f5632dcfde7b32d2012b4351728f6437930887c2f6a91ea242459e5514'

connector_sandbox_namespace_available() {
  local unshare_command true_command
  unshare_command="$(command -v unshare 2>/dev/null || true)"
  true_command="$(command -v true 2>/dev/null || true)"
  [[ -n "$unshare_command" && -n "$true_command" ]] || return 1
  "$unshare_command" -U -r -m -n -i -u "$true_command" >/dev/null 2>&1
}

connector_sandbox_run_in_container() {
  local repository_root=$1
  local qualification_script=$2

  if [[ "${TORGNEXA_SANDBOX_IN_CONTAINER:-}" == "1" ]]; then
    echo "connector sandbox runtime qualification: FAIL (namespace preflight failed inside qualification container)" >&2
    return 1
  fi
  if [[ -n "${CI:-}" || -n "${GITHUB_ACTIONS:-}" ]] && [[ "${TORGNEXA_SANDBOX_ALLOW_CONTAINER_FALLBACK:-}" != "1" ]]; then
    echo "connector sandbox runtime qualification: FAIL (CI runner cannot create Linux namespaces; use a namespace-enabled runner)" >&2
    return 1
  fi
  if ! command -v docker >/dev/null 2>&1; then
    echo "connector sandbox runtime qualification: FAIL (Linux namespaces unavailable and Docker is missing)" >&2
    return 1
  fi

  echo "connector sandbox runtime qualification: host namespaces unavailable; using pinned qualification container"
  docker run --rm --pull=missing --network none --read-only \
    --cap-add=SYS_ADMIN --security-opt=apparmor=unconfined \
    --pids-limit 128 --memory 512m --cpus 2 \
    --tmpfs /tmp:rw,nosuid,nodev,exec \
    --mount "type=bind,src=$repository_root,dst=/app,readonly" \
    --workdir /app \
    --env GOTOOLCHAIN=local \
    --env GOWORK=off \
    --env GOCACHE=/tmp/go-cache \
    --env GOMODCACHE=/tmp/go-mod-cache \
    --env GOPATH=/tmp/go-path \
    --env HOME=/tmp/home \
    --env TMPDIR=/tmp \
    --env TORGNEXA_SANDBOX_IN_CONTAINER=1 \
    "$TORGNEXA_SANDBOX_QUALIFICATION_IMAGE" \
    bash -c 'export PATH=/usr/local/go/bin:$PATH; exec bash "$1"' bash "/app/$qualification_script"
}
