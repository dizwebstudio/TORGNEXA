#!/usr/bin/env bash
set -euo pipefail
umask 077
export LC_ALL=C TZ=UTC
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
evidence=${EVIDENCE_DIR:-}
fail(){ echo "GitHub release stager: $*" >&2; exit 1; }
[[ "$evidence" == /* && -d "$evidence" && ! -L "$evidence" ]] || fail "EVIDENCE_DIR must be an absolute non-symlink directory"
for cmd in curl jq mktemp sha256sum; do command -v "$cmd" >/dev/null || fail "$cmd is required"; done
[[ -n "${GITHUB_TOKEN:-}" && -n "${GITHUB_REPOSITORY:-}" && -n "${GITHUB_REF:-}" && -n "${GITHUB_SHA:-}" ]] || fail "GitHub release context/token missing"
[[ "${GITHUB_EVENT_NAME:-}" == push ]] || fail "public publication is permitted only from a tag push"
manifest="$evidence/evidence.json"; [[ -f "$manifest" && ! -L "$manifest" ]] || fail "evidence.json missing"
repository="$(jq -er '.release.repository' "$manifest")"; version="$(jq -er '.release.version' "$manifest")"; commit="$(jq -er '.release.commit' "$manifest")"; ref="$(jq -er '.release.ref' "$manifest")"
[[ "$repository" == "$GITHUB_REPOSITORY" && "$commit" == "$GITHUB_SHA" && "$ref" == "$GITHUB_REF" && "$ref" == "refs/tags/$version" ]] || fail "evidence does not match immutable GitHub release context"
jq -e '.release.public_ready == true and (.blockers|length)==0 and .external_identity_verification.status=="pass"' "$manifest" >/dev/null || fail "evidence is not public-ready"
api="https://api.github.com/repos/$repository"; auth=(-H "Authorization: Bearer $GITHUB_TOKEN" -H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2026-03-10')
status=$(curl -sS -o /dev/null -w '%{http_code}' "${auth[@]}" "$api/releases/tags/$version")
[[ "$status" == 404 ]] || fail "release tag already exists or cannot be safely checked (HTTP $status)"
tmp="$(mktemp -d "${RUNNER_TEMP:-/tmp}/torgnexa-publish.XXXXXX")"; trap 'rm -rf -- "$tmp"' EXIT
bundle="$tmp/torgnexa_${version#v}_release-evidence.tar.gz"
"$root/scripts/package-release-evidence.sh" --evidence-dir "$evidence" --output "$bundle" >/dev/null
body="$(jq -cn --arg tag "$version" --arg name "TORGNEXA $version" --arg commit "$commit" '{tag_name:$tag,target_commitish:$commit,name:$name,body:"Signed TORGNEXA release. Verify the attached evidence bundle and Sigstore attestations before deployment.",draft:true,prerelease:false,generate_release_notes:false}')"
response="$tmp/create.json"
curl --fail-with-body -sS "${auth[@]}" -H 'Content-Type: application/json' -d "$body" "$api/releases" >"$response"
id="$(jq -er '.id' "$response")"; upload="$(jq -er '.upload_url' "$response")"; upload=${upload%%\{*}
[[ "$id" =~ ^[0-9]+$ && "$upload" == https://uploads.github.com/* ]] || fail "unsafe release response"
upload_asset(){ local file=$1 name=$2 type=$3; [[ "$name" =~ ^[A-Za-z0-9._-]+$ ]] || fail "unsafe asset name"; curl --fail-with-body -sS "${auth[@]}" -H "Content-Type: $type" --data-binary "@$file" "$upload?name=$name" >/dev/null; }
upload_asset "$manifest" evidence.json application/json
upload_asset "$bundle" "$(basename "$bundle")" application/gzip
while IFS=$'\t' read -r path name; do [[ "$path" =~ ^artifacts/[A-Za-z0-9._-]+$ ]] || fail "unsafe subject path"; upload_asset "$evidence/$path" "$name" application/octet-stream; done < <(jq -r '.subjects[] | select(.type=="binary") | [.path,(.path|split("/")[-1])] | @tsv' "$manifest")
echo "GitHub release stager: PASS ($version draft_id=$id)"
