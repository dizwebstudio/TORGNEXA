#!/usr/bin/env bash
set -euo pipefail

umask 077

trivy_path=""

usage() {
  echo "usage: $0 --trivy ABSOLUTE_BINARY_PATH" >&2
}

die() {
  echo "check-secret-canary: $*" >&2
  exit 1
}

while (($# > 0)); do
  case "$1" in
    --trivy)
      (($# >= 2)) || { usage; exit 2; }
      trivy_path=$2
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

[[ "$trivy_path" == /* && -x "$trivy_path" && -f "$trivy_path" && ! -L "$trivy_path" ]] || \
  die "--trivy must identify an executable absolute regular file"
command -v jq >/dev/null 2>&1 || die "required command not found: jq"

work_dir="$(mktemp -d)"
cleanup() {
  if [[ -n "${work_dir:-}" && -d "$work_dir" ]]; then
    rm -rf -- "$work_dir"
  fi
}
trap cleanup EXIT HUP INT TERM

mkdir -m 0700 -- "$work_dir/fixture"
printf '%s\n' \
  'rules:' \
  '  - id: torgnexa-secret-canary' \
  '    category: canary' \
  '    title: TORGNEXA secret scanner canary' \
  '    severity: CRITICAL' \
  '    keywords:' \
  '      - TORGNEXA_SCANNER_CANARY' \
  "    regex: 'TORGNEXA_SCANNER_CANARY_[A-Z0-9]{32}'" \
  >"$work_dir/secret-config.yaml"
printf '%s\n' 'TORGNEXA_SCANNER_CANARY_0123456789ABCDEF0123456789ABCDEF' \
  >"$work_dir/fixture/canary.txt"
printf 'export AWS_ACCESS_KEY_ID=%s%s\n' 'AKIA' 'Z7YH4G2J8M6Q3R5T' \
  >>"$work_dir/fixture/canary.txt"

"$trivy_path" fs --scanners secret \
  --secret-config "$work_dir/secret-config.yaml" \
  --format json --output "$work_dir/result.json" --exit-code 0 \
  "$work_dir/fixture" >/dev/null 2>&1 || die "Trivy failed while running the secret-scanner canary"

jq -e '[.Results[]?.Secrets[]? | select(.RuleID == "torgnexa-secret-canary")] | length == 1' \
  "$work_dir/result.json" >/dev/null || die "Trivy secret scanner did not detect its synthetic canary"
jq -e '[.Results[]?.Secrets[]? | select(.RuleID == "aws-access-key-id")] | length == 1' \
  "$work_dir/result.json" >/dev/null || die "Trivy built-in secret rules did not detect their synthetic canary"

echo "Trivy secret scanner canary detected"
