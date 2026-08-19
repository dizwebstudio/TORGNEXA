#!/usr/bin/env bash
set -euo pipefail

umask 077
export CGO_ENABLED=0
export GOARCH=amd64
export GOOS=linux
export GOTELEMETRY=off
export GOTOOLCHAIN=local
export GOWORK=off
export LC_ALL=C
export TZ=UTC

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
inventory="$repo_root/supply-chain/release-artifacts.json"
mode="dry-run"
release_version=""
source_revision=""
source_date_epoch=""
default_branch=""
output_dir=""

usage() {
  echo "usage: $0 [--mode dry-run|public] --version SEMVER --source-revision 40_HEX --source-date-epoch UNIX_SECONDS [--default-branch BRANCH] --output-dir ABSOLUTE_EMPTY_DIRECTORY" >&2
}

die() {
  echo "build-release: $*" >&2
  exit 1
}

while (($# > 0)); do
  case "$1" in
    --mode)
      (($# >= 2)) || { usage; exit 2; }
      mode=$2
      shift 2
      ;;
    --version)
      (($# >= 2)) || { usage; exit 2; }
      release_version=$2
      shift 2
      ;;
    --source-revision)
      (($# >= 2)) || { usage; exit 2; }
      source_revision=$2
      shift 2
      ;;
    --source-date-epoch)
      (($# >= 2)) || { usage; exit 2; }
      source_date_epoch=$2
      shift 2
      ;;
    --default-branch)
      (($# >= 2)) || { usage; exit 2; }
      default_branch=$2
      shift 2
      ;;
    --output-dir)
      (($# >= 2)) || { usage; exit 2; }
      output_dir=$2
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
"$repo_root/scripts/check-semver.sh" "$release_version" >/dev/null || \
  die "--version must be a canonical SemVer without a v prefix"
[[ "$source_revision" =~ ^[0-9a-f]{40}$ ]] || die "--source-revision must be 40 lowercase hexadecimal characters"
[[ "$source_date_epoch" =~ ^(0|[1-9][0-9]{0,10})$ ]] || \
  die "--source-date-epoch must be a non-negative Unix timestamp"
[[ -n "$output_dir" && "$output_dir" == /* ]] || die "--output-dir must be an absolute path"
[[ -d "$output_dir" && ! -L "$output_dir" ]] || \
  die "--output-dir must be an existing non-symlink directory"

safe_root=${TORGNEXA_SAFE_OUTPUT_ROOT:-${RUNNER_TEMP:-/tmp}}
[[ "$safe_root" == /* && -d "$safe_root" && ! -L "$safe_root" ]] || \
  die "safe output root must be an existing absolute non-symlink directory"
safe_root="$(realpath -e -- "$safe_root")"
output_dir="$(realpath -e -- "$output_dir")"
[[ "$output_dir" == "$safe_root/"* ]] || die "--output-dir must be a child of $safe_root"
[[ -z "$(find "$output_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]] || die "--output-dir must be empty"

"$repo_root/scripts/check-js-supply-chain.sh" release

for command_name in cmp find go jq mktemp realpath sha256sum sort tr wc; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done

go -C "$repo_root/tools/contractcheck" run ./cmd/supplychaincheck -root ../..

source_verified=false
if git -C "$repo_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  head_revision="$(git -C "$repo_root" rev-parse HEAD)"
  if [[ "$head_revision" == "$source_revision" && -z "$(git -C "$repo_root" status --porcelain=v1 --untracked-files=all)" ]]; then
    source_verified=true
  fi
fi

if [[ "$mode" == public ]]; then
  command -v git >/dev/null 2>&1 || die "public release requires git"

  [[ -n "$default_branch" && ! "$default_branch" =~ [[:space:]\\] && "$default_branch" != *..* && "$default_branch" != *'@{'* ]] || \
    die "public release requires a safe explicit default branch"
  git -C "$repo_root" check-ref-format --branch "$default_branch" >/dev/null || \
    die "public release default branch is invalid"
  jq -e '.public_release_ready == true' "$inventory" >/dev/null || \
    die "public release is disabled by supply-chain/release-artifacts.json"
  [[ -f "$repo_root/LICENSE" && ! -L "$repo_root/LICENSE" && -s "$repo_root/LICENSE" ]] || \
    die "public release requires a reviewed, non-empty root LICENSE file"
  [[ "$source_verified" == true ]] || die "public release requires a clean checkout at the requested revision"
  tag_revision="$(git -C "$repo_root" rev-parse "refs/tags/v${release_version}^{commit}" 2>/dev/null)" || \
    die "public release requires the matching v${release_version} tag"
  [[ "$tag_revision" == "$source_revision" ]] || die "release tag does not identify the requested revision"
  default_revision="$(git -C "$repo_root" rev-parse "refs/remotes/origin/${default_branch}^{commit}" 2>/dev/null)" || \
    die "public release requires the fetched default branch"
  git -C "$repo_root" merge-base --is-ancestor "$source_revision" "$default_revision" || \
    die "release commit is not an ancestor of the default branch"
  commit_epoch="$(git -C "$repo_root" show -s --format=%ct "$source_revision")"
  [[ "$commit_epoch" == "$source_date_epoch" ]] || die "SOURCE_DATE_EPOCH does not match the release commit"
fi

export SOURCE_DATE_EPOCH="$source_date_epoch"

work_dir="$(mktemp -d "$safe_root/torgnexa-release-build.XXXXXX")"
cleanup() {
  if [[ -n "${work_dir:-}" && -d "$work_dir" ]]; then
    rm -rf -- "$work_dir"
  fi
}
trap cleanup EXIT HUP INT TERM
mkdir -m 0700 -- "$work_dir/first" "$work_dir/second"
declare -a artifact_names=()

ldflags="-s -w -buildid= -X github.com/torgnexa/torgnexa/internal/platform/domain.version=$release_version"
jq -er '.binaries[] | . as $binary | .platforms[] | [$binary.name, $binary.package, .] | @tsv' \
  "$inventory" >"$work_dir/binaries.tsv"

while IFS=$'\t' read -r name package platform; do
  [[ "$name" =~ ^[a-z][a-z0-9-]*$ ]] || die "unsafe binary name in inventory: $name"
  [[ "$package" == "./cmd/$name" ]] || die "unexpected package for $name: $package"
  [[ "$platform" == "linux/amd64" ]] || die "unsupported release platform: $platform"
  filename="${name}_${release_version}_linux_amd64"
  artifact_names+=("$filename")

  go -C "$repo_root" build -mod=readonly -trimpath -buildvcs=false \
    -ldflags "$ldflags" -o "$work_dir/first/$filename" "$package"
  go -C "$repo_root" build -mod=readonly -trimpath -buildvcs=false \
    -ldflags "$ldflags" -o "$work_dir/second/$filename" "$package"
  cmp --silent -- "$work_dir/first/$filename" "$work_dir/second/$filename" || \
    die "non-reproducible bytes produced for $name"
  chmod 0755 -- "$work_dir/first/$filename"
done <"$work_dir/binaries.tsv"

[[ "${#artifact_names[@]}" == 4 ]] || die "release inventory must produce exactly four binaries"

for artifact in "$work_dir/first"/*; do
  mv -- "$artifact" "$output_dir/"
done

public_ready=false
if [[ "$mode" == public ]]; then
  public_ready=true
fi
jq -n \
  --arg release_version "$release_version" \
  --arg source_revision "$source_revision" \
  --arg mode "$mode" \
  --argjson source_date_epoch "$source_date_epoch" \
  --argjson source_verified "$source_verified" \
  --argjson public_ready "$public_ready" \
  '{
    version: 1,
    release_version: $release_version,
    source_revision: $source_revision,
    source_date_epoch: $source_date_epoch,
    mode: $mode,
    source_verified: $source_verified,
    public_ready: $public_ready,
    comparator_published: false
  }' >"$output_dir/build-context.json"

(
  cd -- "$output_dir"
  sha256sum -- "${artifact_names[@]}" build-context.json | sort -k2
) >"$work_dir/SHA256SUMS"
mv -- "$work_dir/SHA256SUMS" "$output_dir/SHA256SUMS"

actual_binaries="$(find "$output_dir" -mindepth 1 -maxdepth 1 -type f -perm -0100 \
  ! -name '.*' | wc -l | tr -d '[:space:]')"
[[ "$actual_binaries" == 4 ]] || die "release output does not contain exactly four executables"

echo "release binaries built reproducibly in $output_dir"
