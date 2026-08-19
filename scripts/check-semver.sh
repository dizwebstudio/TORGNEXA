#!/usr/bin/env bash
set -euo pipefail

if (($# != 1)); then
  echo "usage: $0 SEMVER_WITHOUT_V_PREFIX" >&2
  exit 2
fi

version=$1
if [[ ! "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?(\+[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]]; then
  echo "invalid canonical SemVer: $version" >&2
  exit 1
fi

core_and_prerelease=${version%%+*}
if [[ "$core_and_prerelease" == *-* ]]; then
  prerelease=${core_and_prerelease#*-}
  IFS=. read -r -a identifiers <<<"$prerelease"
  for identifier in "${identifiers[@]}"; do
    if [[ "$identifier" =~ ^[0-9]+$ && ${#identifier} -gt 1 && "$identifier" == 0* ]]; then
      echo "numeric prerelease identifiers must not contain leading zeroes: $version" >&2
      exit 1
    fi
  done
fi
