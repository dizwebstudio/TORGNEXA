#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
# shellcheck source=scan-supply-chain-lib.sh
source "$script_dir/scan-supply-chain-lib.sh"

for command_name in jq mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || {
    echo "scan-supply-chain-lib-test: required command not found: $command_name" >&2
    exit 1
  }
done

test_dir="$(mktemp -d "${TMPDIR:-/tmp}/torgnexa-scan-supply-chain-test.XXXXXX")"
trap 'rm -rf -- "$test_dir"' EXIT HUP INT TERM

for scanner in vulnerability secret license; do
  raw="$test_dir/${scanner}.raw.json"
  report="$test_dir/${scanner}.json"
  jq -n --arg scanner "$scanner" '{SchemaVersion: 2, ArtifactName: ("synthetic/" + $scanner + ":empty")}' >"$raw"
  sanitize_json "$raw" "$report"
  normalize_trivy_report "$report"
  jq -e '.SchemaVersion == 2 and (.Results == [])' "$report" >/dev/null
done

raw="$test_dir/non-empty.raw.json"
report="$test_dir/non-empty.json"
jq -n '{SchemaVersion: 2, Results: [{Secrets: [{RuleID: "synthetic", Match: "must be redacted"}]}]}' >"$raw"
sanitize_json "$raw" "$report"
normalize_trivy_report "$report"
jq -e '.SchemaVersion == 2 and (.Results | length == 1) and (.Results[0].Secrets[0].RuleID == "synthetic") and (.Results[0].Secrets[0] | has("Match") | not)' "$report" >/dev/null

echo "scan-supply-chain-lib-test: PASS"
