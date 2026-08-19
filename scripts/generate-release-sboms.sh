#!/usr/bin/env bash
set -euo pipefail

umask 077
export LC_ALL=C
export SYFT_CHECK_FOR_APP_UPDATE=false
export TZ=UTC

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
inventory="$repo_root/supply-chain/release-artifacts.json"
tool_manifest="$repo_root/supply-chain/tool-versions.json"
artifact_dir=""
release_version=""
syft_path=""

usage() {
  echo "usage: $0 --artifact-dir ABSOLUTE_DIRECTORY --version SEMVER --syft ABSOLUTE_BINARY_PATH" >&2
}

die() {
  echo "generate-release-sboms: $*" >&2
  exit 1
}

while (($# > 0)); do
  case "$1" in
    --artifact-dir)
      (($# >= 2)) || { usage; exit 2; }
      artifact_dir=$2
      shift 2
      ;;
    --version)
      (($# >= 2)) || { usage; exit 2; }
      release_version=$2
      shift 2
      ;;
    --syft)
      (($# >= 2)) || { usage; exit 2; }
      syft_path=$2
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

"$repo_root/scripts/check-semver.sh" "$release_version" >/dev/null || \
  die "--version must be a canonical SemVer without a v prefix"
[[ "$artifact_dir" == /* && -d "$artifact_dir" && ! -L "$artifact_dir" ]] || \
  die "--artifact-dir must be an existing absolute non-symlink directory"
[[ "$syft_path" == /* && -f "$syft_path" && -x "$syft_path" && ! -L "$syft_path" ]] || \
  die "--syft must identify an executable absolute regular file"

safe_root=${TORGNEXA_SAFE_OUTPUT_ROOT:-${RUNNER_TEMP:-/tmp}}
[[ "$safe_root" == /* && -d "$safe_root" && ! -L "$safe_root" ]] || \
  die "safe output root must be an existing absolute non-symlink directory"
safe_root="$(realpath -e -- "$safe_root")"
artifact_dir="$(realpath -e -- "$artifact_dir")"
[[ "$artifact_dir" == "$safe_root/"* ]] || die "--artifact-dir must be a child of $safe_root"

for command_name in jq mktemp realpath sha256sum sort; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done

expected_syft_sha="$(jq -er '.archive_tools[] | select(.name == "syft") | .binary_sha256' "$tool_manifest")" || \
  die "Syft is not registered exactly once"
actual_syft_sha="$(sha256sum -- "$syft_path")"
actual_syft_sha=${actual_syft_sha%% *}
[[ "$actual_syft_sha" == "$expected_syft_sha" ]] || die "Syft checksum mismatch"

[[ -f "$artifact_dir/SHA256SUMS" && ! -L "$artifact_dir/SHA256SUMS" ]] || die "SHA256SUMS is missing"
(
  cd -- "$artifact_dir"
  sha256sum --check --strict SHA256SUMS
) >/dev/null
jq -e --arg version "$release_version" '.release_version == $version' \
  "$artifact_dir/build-context.json" >/dev/null || die "build context does not match --version"

work_dir="$(mktemp -d "$safe_root/torgnexa-sbom-work.XXXXXX")"
cleanup() {
  if [[ -n "${work_dir:-}" && -d "$work_dir" ]]; then
    rm -rf -- "$work_dir"
  fi
}
trap cleanup EXIT HUP INT TERM
mkdir -m 0700 -- "$work_dir/cache"
export XDG_CACHE_HOME="$work_dir/cache"
export SYFT_CACHE_DIR="$work_dir/cache/syft"

jq -er '.binaries[] | . as $binary | .platforms[] | [$binary.name, .] | @tsv' \
  "$inventory" >"$work_dir/binaries.tsv"
declare -a sbom_names=()
binary_count=0
while IFS=$'\t' read -r name platform; do
  [[ "$name" =~ ^[a-z][a-z0-9-]*$ ]] || die "unsafe binary name: $name"
  [[ "$platform" == linux/amd64 ]] || die "unsupported binary platform: $platform"
  binary_count=$((binary_count + 1))
  binary_name="${name}_${release_version}_linux_amd64"
  binary_path="$artifact_dir/$binary_name"
  sbom_name="${binary_name}.spdx.json"
  raw_sbom="$work_dir/$sbom_name"
  [[ -f "$binary_path" && -x "$binary_path" && ! -L "$binary_path" ]] || \
    die "release binary is missing or unsafe: $binary_name"

  "$syft_path" scan "file:$binary_path" \
    --source-name "$name" --source-version "$release_version" \
    --output "spdx-json@2.3=$raw_sbom" >/dev/null
  jq -e '.spdxVersion == "SPDX-2.3" and (.packages | type == "array" and length > 0)' \
    "$raw_sbom" >/dev/null || die "Syft produced an invalid or empty SPDX 2.3 SBOM for $name"
  mv -- "$raw_sbom" "$artifact_dir/$sbom_name"
  sbom_names+=("$sbom_name")
done <"$work_dir/binaries.tsv"

[[ "$binary_count" == 4 && "${#sbom_names[@]}" == 4 ]] || die "expected exactly four binary SBOMs"
(
  cd -- "$artifact_dir"
  sha256sum -- "${sbom_names[@]}" | sort -k2
) >"$work_dir/SBOM_SHA256SUMS"
mv -- "$work_dir/SBOM_SHA256SUMS" "$artifact_dir/SBOM_SHA256SUMS"

echo "four SPDX 2.3 JSON SBOMs generated in $artifact_dir"
