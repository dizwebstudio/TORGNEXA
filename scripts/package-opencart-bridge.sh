#!/usr/bin/env bash
set -euo pipefail

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
source_dir="$root/connectors/storefronts/opencart/extension/torgnexa"
output="${1:-$root/dist/torgnexa.ocmod.zip}"

# OpenCart derives the extension code (and therefore DIR_EXTENSION path) from
# the package filename, not from install.json.  A differently named archive
# would be extracted to extension/<other-name>/ and the public route
# extension/torgnexa/api/* would stop resolving.
if [[ "$(basename -- "$output")" != "torgnexa.ocmod.zip" ]]; then
	echo "output filename must be exactly torgnexa.ocmod.zip for OpenCart routing" >&2
	exit 1
fi

[[ -f "$source_dir/install.json" ]] || { echo "OpenCart bridge install.json is missing" >&2; exit 1; }
command -v zip >/dev/null 2>&1 || { echo "zip is required to package the OpenCart bridge" >&2; exit 1; }

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
mkdir -p "$stage/catalog/controller/api" "$stage/catalog/model"
cp "$source_dir/install.json" "$stage/install.json"
cp "$source_dir/catalog/controller/api"/*.php "$stage/catalog/controller/api/"
cp "$source_dir/catalog/model/api.php" "$stage/catalog/model/api.php"

mkdir -p "$(dirname -- "$output")"
rm -f -- "$output"
(cd "$stage" && zip -q -r "$output" install.json catalog)
printf 'OpenCart bridge package: %s\n' "$output"
