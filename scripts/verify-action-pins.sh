#!/usr/bin/env bash
set -euo pipefail

export GOTELEMETRY=off
export GOTOOLCHAIN=local
export GOWORK=off

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
manifest="$repo_root/supply-chain/action-pins.json"

die() {
  echo "verify-action-pins: $*" >&2
  exit 1
}

for command_name in curl jq; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done

go -C "$repo_root/tools/contractcheck" run ./cmd/supplychaincheck -root ../..

expected_count="$(jq -er '.actions | length' "$manifest")"
[[ "$expected_count" -gt 0 ]] || die "action manifest must not be empty"
verified_count=0
while IFS=$'\t' read -r name version expected; do
  [[ "$name" =~ ^[a-z0-9_.-]+/[a-z0-9_.-]+$ ]] || die "unsafe action name: $name"
  [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "unsafe action version: $version"
  [[ "$expected" =~ ^[0-9a-f]{40}$ ]] || die "invalid expected commit for $name"

  response="$(curl --fail --silent --show-error --location \
    --proto '=https' --tlsv1.2 --retry 3 --retry-all-errors \
    --connect-timeout 10 --max-time 60 \
    -- "https://api.github.com/repos/$name/git/ref/tags/$version")"
  actual="$(jq -er '.object | select(.type == "commit") | .sha' <<<"$response")" || \
    die "tag $name@$version is not a direct commit ref"
  [[ "$actual" == "$expected" ]] || \
    die "tag ref changed for $name@$version: expected $expected, got $actual"
  verified_count=$((verified_count + 1))
done < <(jq -r '.actions[] | [.name, .version, .commit] | @tsv' "$manifest")
[[ "$verified_count" == "$expected_count" ]] || die "not every registered Action pin was verified"

echo "GitHub Action tag refs match the committed full-SHA pins"
