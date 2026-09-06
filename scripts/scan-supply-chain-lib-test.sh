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

os_report="$test_dir/os-vulnerability.json"
language_report="$test_dir/language-vulnerability.json"
merged_report="$test_dir/merged-vulnerability.json"
jq -n '{SchemaVersion: 2, ArtifactName: "synthetic-image", ArtifactType: "container_image", Results: [{Target: "alpine", Class: "os-pkgs", Type: "alpine", Vulnerabilities: []}]}' >"$os_report"
jq -n '{SchemaVersion: 2, ArtifactName: "synthetic-sbom", ArtifactType: "filesystem", Results: [{Target: "synthetic.jar", Class: "lang-pkgs", Type: "jar", Vulnerabilities: [{VulnerabilityID: "CVE-SYNTHETIC"}]}]}' >"$language_report"
merge_trivy_vulnerability_reports "$os_report" "$language_report" \
  "registry.example/synthetic@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  "$merged_report"
jq -e '
  .SchemaVersion == 2 and
  .ArtifactType == "container_image" and
  (.Results | length == 2) and
  ([.Results[]?.Vulnerabilities[]?.VulnerabilityID] | index("CVE-SYNTHETIC") != null)
' "$merged_report" >/dev/null

fake_trivy="$test_dir/fake-trivy"
counter="$test_dir/attempts"
cat >"$fake_trivy" <<'SH'
#!/usr/bin/env sh
set -eu
counter=${TORGNEXA_TEST_ATTEMPT_FILE:?}
attempt=0
if [ -f "$counter" ]; then
  attempt=$(cat "$counter")
fi
attempt=$((attempt + 1))
printf '%s\n' "$attempt" >"$counter"
output=
while [ "$#" -gt 0 ]; do
  if [ "$1" = --output ]; then
    output=$2
    shift 2
    continue
  fi
  shift
done
if [ "$attempt" -lt 3 ]; then
  exit 23
fi
printf '%s\n' '{"SchemaVersion":2}' >"$output"
SH
chmod 0700 "$fake_trivy"
raw="$test_dir/retry.raw.json"
stderr_report="$test_dir/retry.stderr"
TORGNEXA_TEST_ATTEMPT_FILE="$counter" run_command_with_retries \
  3 0 "$fake_trivy" --output "$raw"
[[ "$(cat "$counter")" == 3 ]]
rm -f -- "$counter" "$raw"
TORGNEXA_TEST_ATTEMPT_FILE="$counter" run_trivy_json_with_retries \
  "$fake_trivy" "$raw" "$stderr_report" 3 0 image synthetic
[[ "$(cat "$counter")" == 3 ]]
report="$test_dir/retry.json"
sanitize_json "$raw" "$report"
normalize_trivy_report "$report"
jq -e '.SchemaVersion == 2 and (.Results == [])' "$report" >/dev/null

echo "scan-supply-chain-lib-test: PASS"
