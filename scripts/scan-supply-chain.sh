#!/usr/bin/env bash
set -euo pipefail

umask 077
export CGO_ENABLED=0
export GOTELEMETRY=off
export GOTOOLCHAIN=local
export GOWORK=off
export LC_ALL=C
export TZ=UTC

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
source "$repo_root/scripts/scan-supply-chain-lib.sh"
inventory="$repo_root/supply-chain/release-artifacts.json"
tool_manifest="$repo_root/supply-chain/tool-versions.json"
license_policy="$repo_root/supply-chain/license-policy.json"
risk_exceptions="$repo_root/supply-chain/risk-exceptions.json"
synthetic_secret_policy="$repo_root/supply-chain/synthetic-secret-fixtures.json"
trivy_secret_report_policy="$repo_root/supply-chain/trivy-secret-report-policy.jq"
mode="dry-run"
scope="source"
source_revision=""
output_dir=""
tool_dir=""
trivy_cache_dir=""

usage() {
  echo "usage: $0 [--mode dry-run|public] --scope source|images|all --source-revision 40_HEX --tool-dir ABSOLUTE_DIRECTORY --output-dir ABSOLUTE_EMPTY_DIRECTORY [--trivy-cache-dir ABSOLUTE_DIRECTORY]" >&2
}

die() {
  echo "scan-supply-chain: $*" >&2
  exit 1
}

while (($# > 0)); do
  case "$1" in
    --mode)
      (($# >= 2)) || { usage; exit 2; }
      mode=$2
      shift 2
      ;;
    --scope)
      (($# >= 2)) || { usage; exit 2; }
      scope=$2
      shift 2
      ;;
    --source-revision)
      (($# >= 2)) || { usage; exit 2; }
      source_revision=$2
      shift 2
      ;;
    --tool-dir)
      (($# >= 2)) || { usage; exit 2; }
      tool_dir=$2
      shift 2
      ;;
    --output-dir)
      (($# >= 2)) || { usage; exit 2; }
      output_dir=$2
      shift 2
      ;;
    --trivy-cache-dir)
      (($# >= 2)) || { usage; exit 2; }
      trivy_cache_dir=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

[[ "$mode" == dry-run || "$mode" == public ]] || die "--mode must be dry-run or public"
[[ "$scope" == source || "$scope" == images || "$scope" == all ]] || \
  die "--scope must be source, images, or all"
[[ "$source_revision" =~ ^[0-9a-f]{40}$ ]] || die "--source-revision must be 40 lowercase hexadecimal characters"
[[ "$tool_dir" == /* && -d "$tool_dir" && ! -L "$tool_dir" ]] || \
  die "--tool-dir must be an existing absolute non-symlink directory"
[[ "$output_dir" == /* && -d "$output_dir" && ! -L "$output_dir" ]] || \
  die "--output-dir must be an existing absolute non-symlink directory"

safe_root=${TORGNEXA_SAFE_OUTPUT_ROOT:-${RUNNER_TEMP:-/tmp}}
[[ "$safe_root" == /* && -d "$safe_root" && ! -L "$safe_root" ]] || \
  die "safe output root must be an existing absolute non-symlink directory"
safe_root="$(realpath -e -- "$safe_root")"
tool_dir="$(realpath -e -- "$tool_dir")"
output_dir="$(realpath -e -- "$output_dir")"
[[ "$output_dir" == "$safe_root/"* ]] || die "--output-dir must be a child of $safe_root"
[[ -z "$(find "$output_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]] || die "--output-dir must be empty"
if [[ -n "$trivy_cache_dir" ]]; then
  [[ "$trivy_cache_dir" == /* && -d "$trivy_cache_dir" && ! -L "$trivy_cache_dir" ]] || \
    die "--trivy-cache-dir must be an existing absolute non-symlink directory"
  trivy_cache_dir="$(realpath -e -- "$trivy_cache_dir")"
  [[ "$trivy_cache_dir" == "$safe_root/"* && "$trivy_cache_dir" != "$output_dir" && "$trivy_cache_dir" != "$output_dir/"* ]] || \
    die "--trivy-cache-dir must be a separate child of $safe_root"
fi

for command_name in awk cp date find go jq mktemp python3 realpath sha256sum sleep tail wc; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done

check_sha256() {
  local path=$1
  local expected=$2
  local actual

  actual="$(sha256sum -- "$path")"
  actual=${actual%% *}
  [[ "$actual" == "$expected" ]] || die "tool checksum mismatch: $(basename -- "$path")"
}

for tool_name in cosign syft trivy; do
  tool_path="$tool_dir/$tool_name"
  [[ -f "$tool_path" && -x "$tool_path" && ! -L "$tool_path" ]] || \
    die "required verified tool is missing: $tool_name"
  expected_sha="$(jq -er --arg name "$tool_name" \
    '[.archive_tools[], .binary_tools[]] | map(select(.name == $name)) | select(length == 1) | .[0].binary_sha256' \
    "$tool_manifest")" || die "tool is not uniquely registered: $tool_name"
  check_sha256 "$tool_path" "$expected_sha"
done

trivy="$tool_dir/trivy"
syft="$tool_dir/syft"

jq -e '.exceptions | length == 0' "$risk_exceptions" >/dev/null || \
  die "risk exceptions are not supported until approval enforcement is enabled"

source_verified=false
if git -C "$repo_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  head_revision="$(git -C "$repo_root" rev-parse HEAD)"
  [[ "$head_revision" == "$source_revision" ]] || die "checkout HEAD does not match --source-revision"
  [[ -z "$(git -C "$repo_root" status --porcelain=v1 --untracked-files=all)" ]] || \
    die "supply-chain scans require a clean checkout"
  source_verified=true
elif [[ "$mode" == public ]]; then
  die "release scans require a real git checkout"
fi

work_dir="$(mktemp -d "$safe_root/torgnexa-security-work.XXXXXX")"
cleanup() {
  if [[ -n "${work_dir:-}" && -d "$work_dir" ]]; then
    rm -rf -- "$work_dir"
  fi
}
trap cleanup EXIT HUP INT TERM
mkdir -m 0700 -- "$work_dir/raw"
if [[ -z "$trivy_cache_dir" ]]; then
  trivy_cache_dir="$work_dir/trivy-cache"
  mkdir -m 0700 -- "$trivy_cache_dir"
fi

gate_failed=0
status_file="$work_dir/status.tsv"
: >"$status_file"

record_status() {
  local check_name=$1
  local result=$2
  printf '%s\t%s\n' "$check_name" "$result" >>"$status_file"
  if [[ "$result" != passed ]]; then
    gate_failed=1
  fi
}

validate_govuln_report() {
  local report=$1
  local check_name=$2
  local config updated_at updated_epoch current_epoch

  if ! config="$(jq -cer '
    select(type == "array") |
    [.[] | select(.config != null) | .config] |
    select(length == 1) | .[0] |
    select(
      .protocol_version == "v1.0.0" and
      .scanner_name == "govulncheck" and
      .scanner_version == "v1.6.0" and
      .db == "https://vuln.go.dev" and
      .scan_level == "symbol" and
      .scan_mode == "source" and
      (.db_last_modified | type == "string")
    )
  ' "$report")"; then
    record_status "$check_name-protocol" failed
    return
  fi
  updated_at="$(jq -er '.db_last_modified' <<<"$config")" || {
    record_status "$check_name-database" failed
    return
  }
  updated_epoch="$(date -u -d "$updated_at" +%s)" || {
    record_status "$check_name-database" failed
    return
  }
  current_epoch="$(date -u +%s)"
  # vuln.go.dev reports the last advisory-content change, not the HTTP fetch
  # time. A live scan can legitimately see an unchanged database for several
  # days, so use a bounded 30-day content-age guard and retain the timestamp.
  if ((current_epoch < updated_epoch || current_epoch - updated_epoch > 2592000)); then
    record_status "$check_name-database" failed
    return
  fi
  record_status "$check_name-protocol" passed
}

validate_gosec_report() {
  local report=$1
  local check_name=$2

  if jq -e '
    (.Issues | type == "array") and
    (.Stats | type == "object") and
    (."Golang errors" | type == "object") and
    ([."Golang errors"[]? | length] | add // 0) == 0 and
    (.Stats.found == (.Issues | length)) and
    ([.Issues[]? | select((.severity | ascii_upcase) == "HIGH" or (.severity | ascii_upcase) == "CRITICAL")] | length) == 0
  ' "$report" >/dev/null; then
    record_status "$check_name-policy" passed
  else
    record_status "$check_name-policy" failed
  fi
}

validate_trivy_report() {
  local report=$1
  local check_name=$2
  local scanner=$3
  local policy_status=0

  if ! jq -e '.SchemaVersion == 2 and (.Results | type == "array")' "$report" >/dev/null; then
    record_status "$check_name-policy" failed
    return
  fi
  case "$scanner" in
    vulnerability)
      jq -e '[.Results[]?.Vulnerabilities[]? | select((.Severity | ascii_upcase) == "HIGH" or (.Severity | ascii_upcase) == "CRITICAL")] | length == 0' "$report" >/dev/null || policy_status=$?
      ;;
    misconfiguration)
      jq -e '[.Results[]?.Misconfigurations[]? | select((.Severity | ascii_upcase) == "HIGH" or (.Severity | ascii_upcase) == "CRITICAL")] | length == 0' "$report" >/dev/null || policy_status=$?
      ;;
    secret)
      jq -e -f "$trivy_secret_report_policy" "$report" >/dev/null || policy_status=$?
      ;;
    license)
      true
      ;;
    *)
      die "internal error: unsupported Trivy report type $scanner"
      ;;
  esac
  if ((policy_status == 0)); then
    record_status "$check_name-policy" passed
  else
    record_status "$check_name-policy" failed
  fi
}

enforce_license_policy() {
  local report=$1
  local check_name=$2

  if ! jq -e '.SchemaVersion == 2 and (.Results | type == "array")' "$report" >/dev/null; then
    echo "scan-supply-chain: $check_name has no valid Trivy license report" >&2
    record_status "$check_name-spdx-policy" failed
    return
  fi
  if GOFLAGS=-mod=readonly go -C "$repo_root/tools/contractcheck" run ./cmd/licensecheck \
    -policy "$license_policy" -report "$report" >/dev/null; then
    record_status "$check_name-spdx-policy" passed
  else
    record_status "$check_name-spdx-policy" failed
  fi
}

run_json_check() {
  local check_name=$1
  local destination=$2
  shift 2
  local raw="$work_dir/raw/${check_name}.json"
  local command_status sanitize_status

  set +e
  "$@" >"$raw" 2>"$work_dir/raw/${check_name}.stderr"
  command_status=$?
  set -e
  sanitize_status=0
  sanitize_json "$raw" "$destination" || sanitize_status=$?
  if ((sanitize_status == 0)) && ! jq -e '
    (type == "object" and length > 0) or (type == "array" and length > 0)
  ' "$destination" >/dev/null; then
    sanitize_status=1
  fi
  if ((sanitize_status != 0)); then
    rm -f -- "$destination"
    jq -n --arg check "$check_name" \
      '{version: 1, status: "scanner_error", check: $check}' >"$destination"
    command_status=1
  fi
  if ((command_status == 0)); then
    record_status "$check_name" passed
  else
    record_status "$check_name" failed
  fi
}

run_trivy_json_check() {
  local check_name=$1
  local destination=$2
  shift 2
  local raw="$work_dir/raw/${check_name}.json"
  local command_status sanitize_status

  if run_trivy_json_with_retries "$trivy" "$raw" \
    "$work_dir/raw/${check_name}.stderr" 3 2 "$@"; then
    command_status=0
  else
    command_status=$?
    echo "scan-supply-chain: $check_name failed after 3 scanner attempts" >&2
  fi
  sanitize_status=0
  sanitize_json "$raw" "$destination" || sanitize_status=$?
  if ((sanitize_status == 0 && command_status == 0)); then
    normalize_trivy_report "$destination" || sanitize_status=$?
  fi
  if ((sanitize_status == 0)) && ! jq -e '
    type == "object" and length > 0
  ' "$destination" >/dev/null; then
    sanitize_status=1
  fi
  if ((sanitize_status != 0)); then
    rm -f -- "$destination"
    jq -n --arg check "$check_name" \
      '{version: 1, status: "scanner_error", check: $check}' >"$destination"
    command_status=1
  fi
  if ((command_status == 0)); then
    record_status "$check_name" passed
  else
    record_status "$check_name" failed
  fi
}

run_trivy_secret_check() {
  local check_name=$1
  local destination=$2
  shift 2
  local raw="$work_dir/raw/${check_name}.json"
  local classified="$work_dir/raw/${check_name}.classified.json"
  local command_status sanitize_status

  if run_trivy_json_with_retries "$trivy" "$raw" \
    "$work_dir/raw/${check_name}.stderr" 3 2 "$@"; then
    command_status=0
  else
    command_status=$?
    echo "scan-supply-chain: $check_name failed after 3 scanner attempts" >&2
  fi
  sanitize_status=0
  if ((command_status == 0)); then
    python3 "$repo_root/scripts/classify-trivy-secrets.py" \
      --input "$raw" --policy "$synthetic_secret_policy" --output "$classified" || sanitize_status=$?
  else
    sanitize_status=1
  fi
  if ((sanitize_status == 0)); then
    sanitize_json "$classified" "$destination" || sanitize_status=$?
  fi
  if ((sanitize_status == 0)); then
    normalize_trivy_report "$destination" || sanitize_status=$?
  fi
  if ((sanitize_status != 0)); then
    rm -f -- "$destination"
    jq -n --arg check "$check_name" \
      '{version: 1, status: "scanner_error", check: $check}' >"$destination"
    command_status=1
  fi
  if ((command_status == 0)); then
    record_status "$check_name" passed
  else
    record_status "$check_name" failed
  fi
}

run_image_vulnerability_check() {
  local check_name=$1
  local destination=$2
  local image=$3
  local platform=$4
  local sbom_path=$5
  local os_raw="$work_dir/raw/${check_name}-os.json"
  local os_report="$work_dir/raw/${check_name}-os.sanitized.json"
  local language_raw="$work_dir/raw/${check_name}-language.json"
  local language_report="$work_dir/raw/${check_name}-language.sanitized.json"
  local command_status=0

  if ! run_trivy_json_with_retries "$trivy" "$os_raw" \
    "$work_dir/raw/${check_name}-os.stderr" 3 2 \
    image --image-src remote --cache-dir "$trivy_cache_dir" \
    --skip-db-update --skip-java-db-update --platform "$platform" \
    --scanners vuln --pkg-types os --detection-priority precise \
    --exit-on-eol 1 --exit-code 0 "$image"; then
    command_status=1
    echo "scan-supply-chain: $check_name OS scan failed after 3 scanner attempts" >&2
  fi
  if ! run_trivy_json_with_retries "$trivy" "$language_raw" \
    "$work_dir/raw/${check_name}-language.stderr" 3 2 \
    sbom --cache-dir "$trivy_cache_dir" --skip-db-update --skip-java-db-update \
    --scanners vuln --pkg-types library --detection-priority precise \
    --exit-code 0 "$sbom_path"; then
    command_status=1
    echo "scan-supply-chain: $check_name language scan failed after 3 scanner attempts" >&2
  fi

  if ((command_status == 0)) && \
    sanitize_json "$os_raw" "$os_report" && \
    normalize_trivy_report "$os_report" && \
    sanitize_json "$language_raw" "$language_report" && \
    normalize_trivy_report "$language_report" && \
    merge_trivy_vulnerability_reports "$os_report" "$language_report" \
      "$image" "$destination"; then
    record_status "$check_name" passed
    return
  fi

  rm -f -- "$destination"
  jq -n --arg check "$check_name" \
    '{version: 1, status: "scanner_error", check: $check}' >"$destination"
  record_status "$check_name" failed
}

go -C "$repo_root/tools/contractcheck" run ./cmd/supplychaincheck -root ../..
all_go_module_dirs=(
  "$repo_root"
  "$repo_root/sdk/examples/go"
  "$repo_root/sdk/go"
  "$repo_root/tools/contractcheck"
  "$repo_root/tools/sdkgen"
  "$repo_root/tools/securitytools"
)
scan_go_module_names=(root sdk-examples-go sdk-go contractcheck sdkgen)
scan_go_module_dirs=(
  "$repo_root"
  "$repo_root/sdk/examples/go"
  "$repo_root/sdk/go"
  "$repo_root/tools/contractcheck"
  "$repo_root/tools/sdkgen"
)
scan_go_module_relative_from_tools=(../.. ../../sdk/examples/go ../../sdk/go ../contractcheck ../sdkgen)
for module_dir in "${all_go_module_dirs[@]}"; do
  go -C "$module_dir" mod tidy -diff
  GOFLAGS=-mod=readonly go -C "$module_dir" mod download
  go -C "$module_dir" mod verify
done
if [[ -n "$(find "$repo_root/tools/securitytools" -type f -name '*.go' -print -quit)" ]]; then
  die "tools/securitytools unexpectedly contains source packages; add it to the SAST scan set"
fi

run_command_with_retries 3 2 "$trivy" image --cache-dir "$trivy_cache_dir" \
  --download-db-only >/dev/null || die "Trivy vulnerability database download failed after 3 attempts"
trivy_update_flags=(--skip-db-update)

metadata="$trivy_cache_dir/db/metadata.json"
[[ -f "$metadata" && ! -L "$metadata" ]] || die "Trivy vulnerability database metadata is missing"
database_file="$trivy_cache_dir/db/trivy.db"
[[ -f "$database_file" && ! -L "$database_file" ]] || die "Trivy vulnerability database file is missing"
updated_at="$(jq -er '.UpdatedAt' "$metadata")" || die "Trivy vulnerability database metadata is malformed"
updated_epoch="$(date -u -d "$updated_at" +%s)" || die "invalid Trivy database timestamp"
now_epoch="$(date -u +%s)"
((now_epoch >= updated_epoch && now_epoch - updated_epoch <= 172800)) || \
  die "Trivy vulnerability database is older than 48 hours"
sanitize_json "$metadata" "$output_dir/trivy-db-metadata.json"
database_metadata_sha="$(sha256sum -- "$output_dir/trivy-db-metadata.json")"
database_metadata_sha=${database_metadata_sha%% *}
database_content_sha="$(sha256sum -- "$database_file")"
database_content_sha=${database_content_sha%% *}
generated_at="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
trivy_version="$(jq -er '.archive_tools[] | select(.name == "trivy") | .version' "$tool_manifest")"
syft_version="$(jq -er '.archive_tools[] | select(.name == "syft") | .version' "$tool_manifest")"
gosec_version="$(jq -er '.go_tools[] | select(.name == "gosec") | .version' "$tool_manifest")"
govulncheck_version="$(jq -er '.go_tools[] | select(.name == "govulncheck") | .version' "$tool_manifest")"

if [[ "$scope" == source || "$scope" == all ]]; then
  "$repo_root/scripts/check-secret-canary.sh" --trivy "$trivy"

  for module_index in "${!scan_go_module_names[@]}"; do
    module_name="${scan_go_module_names[$module_index]}"
    module_dir="${scan_go_module_dirs[$module_index]}"
    module_relative="${scan_go_module_relative_from_tools[$module_index]}"
    run_json_check "govuln-$module_name" "$output_dir/govuln-$module_name.json" \
      env GOFLAGS=-mod=readonly go -C "$repo_root/tools/securitytools" tool govulncheck \
        -C "$module_relative" -test -format json ./...
    validate_govuln_report "$output_dir/govuln-$module_name.json" "govuln-$module_name"
  done
  record_status go-module-securitytools-no-runtime-packages passed

  run_json_check gosec-root "$output_dir/gosec-root.json" \
    env GOFLAGS=-mod=readonly go -C "$repo_root/tools/securitytools" tool gosec -no-fail -tests \
      -nosec-require-rules -nosec-require-justification -fmt=json \
      -exclude-dir="$repo_root/sdk" -exclude-dir="$repo_root/tools" "$repo_root/..."
  validate_gosec_report "$output_dir/gosec-root.json" gosec-root
  for module_index in 1 2 3 4; do
    module_name="${scan_go_module_names[$module_index]}"
    module_dir="${scan_go_module_dirs[$module_index]}"
    run_json_check "gosec-$module_name" "$output_dir/gosec-$module_name.json" \
      env GOFLAGS=-mod=readonly go -C "$repo_root/tools/securitytools" tool gosec -no-fail -tests \
        -nosec-require-rules -nosec-require-justification -fmt=json "$module_dir/..."
    validate_gosec_report "$output_dir/gosec-$module_name.json" "gosec-$module_name"
  done

  run_trivy_json_check trivy-source-vulnerability "$output_dir/trivy-source-vulnerability.json" \
    fs --cache-dir "$trivy_cache_dir" "${trivy_update_flags[@]}" \
    --scanners vuln --exit-code 0 "$repo_root"
  validate_trivy_report "$output_dir/trivy-source-vulnerability.json" trivy-source-vulnerability vulnerability
  run_trivy_json_check trivy-source-license "$output_dir/trivy-source-license.json" \
    fs --cache-dir "$trivy_cache_dir" "${trivy_update_flags[@]}" \
    --scanners license --license-full --exit-code 0 "$repo_root"
  validate_trivy_report "$output_dir/trivy-source-license.json" trivy-source-license license
  enforce_license_policy "$output_dir/trivy-source-license.json" trivy-source-license
  run_trivy_json_check trivy-source-misconfiguration "$output_dir/trivy-source-misconfiguration.json" \
    fs --cache-dir "$trivy_cache_dir" "${trivy_update_flags[@]}" \
    --scanners misconfig --exit-code 0 "$repo_root"
  validate_trivy_report "$output_dir/trivy-source-misconfiguration.json" trivy-source-misconfiguration misconfiguration
  run_trivy_secret_check trivy-source-secret "$output_dir/trivy-source-secret.json" \
    fs --cache-dir "$trivy_cache_dir" "${trivy_update_flags[@]}" \
    --scanners secret --exit-code 0 "$repo_root"
  validate_trivy_report "$output_dir/trivy-source-secret.json" trivy-source-secret secret

  source_status=passed
  ((gate_failed == 0)) || source_status=failed
  jq -Rn \
    --arg revision "$source_revision" \
    --arg status "$source_status" \
    --arg generated_at "$generated_at" \
    --arg updated_at "$updated_at" \
    --arg database_metadata_sha256 "$database_metadata_sha" \
    --arg database_digest "$database_content_sha" \
    --arg trivy "$trivy_version" \
    --arg syft "$syft_version" \
    --arg gosec "$gosec_version" \
    --arg govulncheck "$govulncheck_version" \
    --argjson source_verified "$source_verified" '
    [inputs | split("\t") | {check: .[0], status: .[1]}] |
    {
      version: 1,
      scope: "source",
      source_revision: $revision,
      source_verified: $source_verified,
      generated_at: $generated_at,
      status: $status,
      tools: {trivy: $trivy, syft: $syft, gosec: $gosec, govulncheck: $govulncheck},
      database: {
        uri: "https://github.com/aquasecurity/trivy-db",
        updated_at: $updated_at,
        digest: $database_digest,
        metadata_sha256: $database_metadata_sha256
      },
      checks: .
    }
  ' <"$status_file" >"$output_dir/source-gate.json"
fi

if [[ "$scope" == images || "$scope" == all ]]; then
  image_status_start="$(wc -l <"$status_file")"
  jq -er '.development_runtime[] | . as $runtime | .platforms[] |
    [$runtime.name, $runtime.image, .] | @tsv' "$inventory" >"$work_dir/images.tsv"
  image_count=0
  declare -A scanned_image_platforms=()
  while IFS=$'\t' read -r name image platform; do
    [[ "$name" =~ ^[a-z][a-z0-9-]*$ ]] || die "unsafe image name: $name"
    [[ "$platform" == linux/amd64 || "$platform" == linux/arm64 ]] || die "unsupported image platform: $platform"
    [[ "$image" =~ @sha256:[0-9a-f]{64}$ ]] || die "image is not digest-pinned: $name"
    image_count=$((image_count + 1))
    report_stem="${name}_${platform//\//_}"
    sbom_raw="$work_dir/raw/${report_stem}.spdx.json"
    sbom_path="$output_dir/${report_stem}.spdx.json"
    scan_key="$image|$platform"
    cached_report_stem="${scanned_image_platforms[$scan_key]:-}"
    if [[ -n "$cached_report_stem" ]]; then
      for suffix in spdx.json vulnerability.json license.json secret.json; do
        cp -- "$output_dir/${cached_report_stem}.${suffix}" \
          "$output_dir/${report_stem}.${suffix}"
      done
      if jq -e '.spdxVersion == "SPDX-2.3"' "$sbom_path" >/dev/null; then
        record_status "syft-${report_stem}" passed
      else
        record_status "syft-${report_stem}" failed
      fi
      validate_trivy_report "$output_dir/${report_stem}.vulnerability.json" \
        "trivy-image-vulnerability-${report_stem}" vulnerability
      validate_trivy_report "$output_dir/${report_stem}.license.json" \
        "trivy-image-license-${report_stem}" license
      enforce_license_policy "$output_dir/${report_stem}.license.json" \
        "trivy-image-license-${report_stem}"
      validate_trivy_report "$output_dir/${report_stem}.secret.json" \
        "trivy-image-secret-${report_stem}" secret
      continue
    fi
    scanned_image_platforms["$scan_key"]="$report_stem"

    set +e
    "$syft" scan "registry:$image" --platform "$platform" \
      --source-name "$image" --output "spdx-json@2.3=$sbom_raw" \
      >/dev/null 2>"$work_dir/raw/${report_stem}-syft.stderr"
    syft_status=$?
    set -e
    if ((syft_status == 0)) && jq -e '.spdxVersion == "SPDX-2.3"' "$sbom_raw" >/dev/null; then
      mv -- "$sbom_raw" "$sbom_path"
      record_status "syft-${report_stem}" passed
    else
      jq -n --arg subject "$report_stem" \
        '{version: 1, status: "scanner_error", subject: $subject}' >"$sbom_path"
      record_status "syft-${report_stem}" failed
    fi

    run_image_vulnerability_check "trivy-image-vulnerability-${report_stem}" \
      "$output_dir/${report_stem}.vulnerability.json" "$image" "$platform" "$sbom_path"
    validate_trivy_report "$output_dir/${report_stem}.vulnerability.json" \
      "trivy-image-vulnerability-${report_stem}" vulnerability
    run_trivy_json_check "trivy-image-license-${report_stem}" \
      "$output_dir/${report_stem}.license.json" \
      image --image-src remote --cache-dir "$trivy_cache_dir" "${trivy_update_flags[@]}" \
      --platform "$platform" --scanners license --license-full --exit-code 0 "$image"
    validate_trivy_report "$output_dir/${report_stem}.license.json" \
      "trivy-image-license-${report_stem}" license
    enforce_license_policy "$output_dir/${report_stem}.license.json" "trivy-image-license-${report_stem}"
    run_trivy_json_check "trivy-image-secret-${report_stem}" \
      "$output_dir/${report_stem}.secret.json" \
      image --image-src remote --cache-dir "$trivy_cache_dir" "${trivy_update_flags[@]}" \
      --platform "$platform" --scanners secret --exit-code 0 "$image"
    validate_trivy_report "$output_dir/${report_stem}.secret.json" \
      "trivy-image-secret-${report_stem}" secret
  done <"$work_dir/images.tsv"
  expected_image_count="$(wc -l <"$work_dir/images.tsv")"
  [[ "$image_count" == "$expected_image_count" ]] || \
    die "expected $expected_image_count pinned image/platform scans, got $image_count"

  image_status=passed
  image_failed="$(tail -n "+$((image_status_start + 1))" "$status_file" | awk -F '\t' '$2 != "passed" {count++} END {print count+0}')"
  [[ "$image_failed" == 0 ]] || image_status=failed
  tail -n "+$((image_status_start + 1))" "$status_file" | \
    jq -Rn \
      --arg revision "$source_revision" \
      --arg status "$image_status" \
      --arg generated_at "$generated_at" \
      --arg updated_at "$updated_at" \
      --arg database_metadata_sha256 "$database_metadata_sha" \
      --arg database_digest "$database_content_sha" \
      --arg trivy "$trivy_version" \
      --arg syft "$syft_version" \
      --argjson source_verified "$source_verified" '
      [inputs | split("\t") | {check: .[0], status: .[1]}] |
      {
        version: 1,
        scope: "images",
        source_revision: $revision,
        source_verified: $source_verified,
        generated_at: $generated_at,
        status: $status,
        tools: {trivy: $trivy, syft: $syft},
        database: {
          uri: "https://github.com/aquasecurity/trivy-db",
          updated_at: $updated_at,
          digest: $database_digest,
          metadata_sha256: $database_metadata_sha256
        },
        checks: .
      }
    ' >"$output_dir/image-gate.json"
fi

if ((gate_failed != 0)); then
  echo "one or more supply-chain checks failed; sanitized reports are in $output_dir" >&2
  exit 1
fi

echo "supply-chain scan passed; sanitized reports are in $output_dir"
