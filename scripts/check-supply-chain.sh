#!/usr/bin/env bash
set -euo pipefail

export GOTELEMETRY=off
export GOTOOLCHAIN=local
export GOWORK=off

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"

"$repo_root/scripts/check-js-supply-chain.sh" repository

for version in 0.0.0 1.2.3-0 1.2.3-alpha.0 1.2.3-rc-1+build.7; do
  "$repo_root/scripts/check-semver.sh" "$version"
done
for version in v1.2.3 01.2.3 1.02.3 1.2.03 1.2.3-01 1.2.3-alpha.01 1.2.3-a..b; do
  if "$repo_root/scripts/check-semver.sh" "$version" >/dev/null 2>&1; then
    echo "invalid SemVer accepted: $version" >&2
    exit 1
  fi
done

for module in "$repo_root" "$repo_root/tools/contractcheck" "$repo_root/tools/securitytools"; do
  go -C "$module" mod tidy -diff
  GOFLAGS=-mod=readonly go -C "$module" mod download
  go -C "$module" mod verify
done

go -C "$repo_root/tools/contractcheck" test -mod=readonly ./internal/checker
go -C "$repo_root/tools/contractcheck" run -mod=readonly ./cmd/supplychaincheck -root ../..
