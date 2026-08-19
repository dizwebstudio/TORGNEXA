#!/usr/bin/env bash
set -euo pipefail

umask 077

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
manifest="$repo_root/supply-chain/tool-versions.json"
bin_dir=""
offline_cache=""

usage() {
  echo "usage: $0 --bin-dir ABSOLUTE_DIRECTORY [--offline-cache ABSOLUTE_DIRECTORY]" >&2
}

die() {
  echo "install-security-tools: $*" >&2
  exit 1
}

while (($# > 0)); do
  case "$1" in
    --bin-dir)
      (($# >= 2)) || { usage; exit 2; }
      bin_dir=$2
      shift 2
      ;;
    --offline-cache)
      (($# >= 2)) || { usage; exit 2; }
      offline_cache=$2
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

[[ -n "$bin_dir" && "$bin_dir" == /* ]] || die "--bin-dir must be an absolute path"
[[ -d "$bin_dir" && ! -L "$bin_dir" ]] || die "--bin-dir must be an existing non-symlink directory"
bin_dir="$(realpath -e -- "$bin_dir")"

if [[ -n "$offline_cache" ]]; then
  [[ "$offline_cache" == /* && -d "$offline_cache" && ! -L "$offline_cache" ]] || \
    die "--offline-cache must be an existing absolute non-symlink directory"
  offline_cache="$(realpath -e -- "$offline_cache")"
fi

[[ "$(uname -s)" == Linux && "$(uname -m)" == x86_64 ]] || \
  die "the locked downloads currently support linux/amd64 only"

for command_name in jq sha256sum tar install mktemp realpath; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done
if [[ -z "$offline_cache" ]]; then
  command -v curl >/dev/null 2>&1 || die "required command not found: curl"
fi

jq -e '
  .version == 1 and
  ([.archive_tools[], .binary_tools[]] | all(
    (.name | type == "string" and length > 0) and
    (.source | type == "string" and startswith("https://github.com/")) and
    (.binary_sha256 | test("^[0-9a-f]{64}$"))
  )) and
  (.archive_tools | all(.archive_sha256 | test("^[0-9a-f]{64}$")))
' "$manifest" >/dev/null || die "invalid tool manifest"

work_dir="$(mktemp -d)"
cleanup() {
  if [[ -n "${work_dir:-}" && -d "$work_dir" ]]; then
    rm -rf -- "$work_dir"
  fi
}
trap cleanup EXIT HUP INT TERM

check_sha256() {
  local path=$1
  local expected=$2
  local actual

  actual="$(sha256sum -- "$path")"
  actual=${actual%% *}
  [[ "$actual" == "$expected" ]] || \
    die "checksum mismatch for $(basename -- "$path"): expected $expected, got $actual"
}

fetch() {
  local source=$1
  local destination=$2
  local filename

  filename=$(basename -- "$source")
  if [[ -n "$offline_cache" ]]; then
    [[ -f "$offline_cache/$filename" && ! -L "$offline_cache/$filename" ]] || \
      die "offline artifact is missing or unsafe: $filename"
    install -m 0600 -- "$offline_cache/$filename" "$destination"
    return
  fi

  curl --fail --silent --show-error --location \
    --proto '=https' --tlsv1.2 --retry 3 --retry-all-errors \
    --connect-timeout 10 --max-time 300 \
    --output "$destination" -- "$source"
}

install_verified_binary() {
  local name=$1
  local source_path=$2
  local expected_binary_sha=$3
  local target="$bin_dir/$name"
  local staged

  check_sha256 "$source_path" "$expected_binary_sha"
  [[ ! -L "$target" ]] || die "refusing to replace symlink: $target"
  staged="$(mktemp "$bin_dir/.${name}.XXXXXX")"
  install -m 0755 -- "$source_path" "$staged"
  check_sha256 "$staged" "$expected_binary_sha"
  mv -f -- "$staged" "$target"
}

jq -er '.archive_tools[] | [.name, .source, .archive_sha256, .binary_sha256] | @tsv' \
  "$manifest" >"$work_dir/archive-tools.tsv"
while IFS=$'\t' read -r name source archive_sha binary_sha; do
  [[ "$name" =~ ^[a-z][a-z0-9-]*$ ]] || die "unsafe archive tool name: $name"
  download="$work_dir/$(basename -- "$source")"
  extract_dir="$work_dir/extract-$name"
  mkdir -m 0700 -- "$extract_dir"
  fetch "$source" "$download"
  check_sha256 "$download" "$archive_sha"
  tar --extract --gzip --file "$download" --directory "$extract_dir" \
    --no-same-owner --no-same-permissions -- "$name"
  [[ -f "$extract_dir/$name" && ! -L "$extract_dir/$name" ]] || \
    die "archive did not contain the expected binary: $name"
  install_verified_binary "$name" "$extract_dir/$name" "$binary_sha"
done <"$work_dir/archive-tools.tsv"

jq -er '.binary_tools[] | [.name, .source, .binary_sha256] | @tsv' \
  "$manifest" >"$work_dir/binary-tools.tsv"
while IFS=$'\t' read -r name source binary_sha; do
  [[ "$name" =~ ^[a-z][a-z0-9-]*$ ]] || die "unsafe binary tool name: $name"
  download="$work_dir/$(basename -- "$source")"
  fetch "$source" "$download"
  install_verified_binary "$name" "$download" "$binary_sha"
done <"$work_dir/binary-tools.tsv"

[[ "$(jq -r '[.archive_tools[], .binary_tools[]] | length' "$manifest")" == 3 ]] || \
  die "tool manifest must contain exactly three downloaded tools"
for required_tool in cosign syft trivy; do
  target="$bin_dir/$required_tool"
  [[ -f "$target" && ! -L "$target" ]] || die "required tool was not installed safely: $required_tool"
  expected="$(jq -er --arg name "$required_tool" \
    '[.archive_tools[], .binary_tools[]] | map(select(.name == $name)) | select(length == 1) | .[0].binary_sha256' \
    "$manifest")" || die "required tool missing from manifest: $required_tool"
  check_sha256 "$target" "$expected"
done

echo "verified security tools installed in $bin_dir"
