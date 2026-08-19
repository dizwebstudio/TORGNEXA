#!/usr/bin/env bash
set -euo pipefail
umask 077
export LC_ALL=C TZ=UTC PYTHONDONTWRITEBYTECODE=1
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
fail(){ echo "GitHub release promoter: $*" >&2; exit 1; }
report=${TORGNEXA_P4_GO_LIVE_EVIDENCE:-}; [[ "$report" == /* && -f "$report" && ! -L "$report" ]] || fail "TORGNEXA_P4_GO_LIVE_EVIDENCE must be an absolute regular file"
for cmd in curl jq git python3 sha256sum; do command -v "$cmd" >/dev/null || fail "$cmd is required"; done
[[ -n "${TORGNEXA_P4_GITHUB_RELEASE_TOKEN:-}" ]] || fail "TORGNEXA_P4_GITHUB_RELEASE_TOKEN is required"
python3 "$root/scripts/p4_root_evidence.py" verify --report "$report" >/dev/null || fail "P4 root evidence failed independent local re-verification"
status="$(jq -er '.status' "$report")"; [[ "$status" == PASS ]] || fail "P4 go-live evidence is not PASS"
repository="$(jq -er '.repository' "$report")"; version="$(jq -er '.release_version' "$report")"; commit="$(jq -er '.release_commit' "$report")"; tag="v$version"
[[ "$repository" =~ ^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$ && "$commit" =~ ^[0-9a-f]{40}$ ]] || fail "invalid P4 identity"
[[ -z "$(git -C "$root" status --porcelain)" ]] || fail "promotion requires a clean tagged source worktree"
[[ "$(git -C "$root" rev-parse --verify HEAD)" == "$commit" ]] || fail "P4 evidence commit does not match source worktree"
[[ "$(git -C "$root" describe --tags --exact-match HEAD 2>/dev/null || true)" == "$tag" ]] || fail "source worktree must be exact tag $tag"
report_dir="$(cd -- "$(dirname -- "$report")" && pwd -P)"; staged="$report_dir/staged-release.json"
[[ -f "$staged" && ! -L "$staged" ]] || fail "staged-release.json missing beside P4 root evidence"
api="https://api.github.com/repos/$repository"; auth=(-H "Authorization: Bearer $TORGNEXA_P4_GITHUB_RELEASE_TOKEN" -H 'Accept: application/vnd.github+json' -H 'X-GitHub-Api-Version: 2026-03-10')
releases="$(mktemp)"; upload_response="$(mktemp)"; trap 'rm -f -- "$releases" "$upload_response"' EXIT
curl --fail-with-body -sS "${auth[@]}" "$api/releases?per_page=100" >"$releases"
count="$(jq --arg tag "$tag" '[.[]|select(.tag_name==$tag)]|length' "$releases")"; [[ "$count" == 1 ]] || fail "expected exactly one staged release for $tag"
id="$(jq -er --arg tag "$tag" '.[]|select(.tag_name==$tag)|.id' "$releases")"; draft="$(jq -er --arg tag "$tag" '.[]|select(.tag_name==$tag)|.draft' "$releases")"; [[ "$draft" == true ]] || fail "release is not a draft"
[[ "$id" == "$(jq -er '.release_id' "$staged")" ]] || fail "current draft release id differs from P4 staged evidence"
upload="$(jq -er --arg tag "$tag" '.[]|select(.tag_name==$tag)|.upload_url' "$releases")"; upload=${upload%%\{*}; [[ "$upload" == https://uploads.github.com/* ]] || fail "unsafe GitHub upload URL"
# The draft must still contain exactly the asset set P4 independently verified: no substitutions or extras.
expected_count="$(jq '.assets|length' "$staged")"; current_count="$(jq --arg tag "$tag" '.[]|select(.tag_name==$tag)|.assets|length' "$releases")"; [[ "$expected_count" == "$current_count" ]] || fail "draft asset count changed after P4 qualification"
while IFS=$'\t' read -r name sha size; do
  [[ "$name" =~ ^[A-Za-z0-9._-]+$ && "$sha" =~ ^[0-9a-f]{64}$ && "$size" =~ ^[0-9]+$ ]] || fail "invalid staged asset evidence"
  matches="$(jq --arg tag "$tag" --arg name "$name" '[.[]|select(.tag_name==$tag)|.assets[]|select(.name==$name)]|length' "$releases")"; [[ "$matches" == 1 ]] || fail "draft asset missing/duplicated: $name"
  remote_digest="$(jq -er --arg tag "$tag" --arg name "$name" '.[]|select(.tag_name==$tag)|.assets[]|select(.name==$name)|.digest' "$releases")"
  remote_size="$(jq -er --arg tag "$tag" --arg name "$name" '.[]|select(.tag_name==$tag)|.assets[]|select(.name==$name)|.size' "$releases")"
  [[ "$remote_digest" == "sha256:$sha" && "$remote_size" == "$size" ]] || fail "draft asset changed after P4 qualification: $name"
done < <(jq -r '.assets[]|[.name,.sha256,(.size|tostring)]|@tsv' "$staged")
# Attach the final root decision only after all staged bytes are re-bound. Any subsequent failure leaves the release as a draft.
root_sha="$(sha256sum "$report" | awk '{print $1}')"; root_size="$(wc -c <"$report" | tr -d ' ')"
[[ "$(jq --arg tag "$tag" '[.[]|select(.tag_name==$tag)|.assets[]|select(.name=="p4-go-live.json")]|length' "$releases")" == 0 ]] || fail "p4-go-live.json already exists on draft"
curl --fail-with-body -sS "${auth[@]}" -H 'Content-Type: application/json' --data-binary "@$report" "$upload?name=p4-go-live.json" >"$upload_response"
[[ "$(jq -er '.state' "$upload_response")" == uploaded ]] || fail "P4 root evidence asset did not reach uploaded state"
[[ "$(jq -er '.digest' "$upload_response")" == "sha256:$root_sha" && "$(jq -er '.size' "$upload_response")" == "$root_size" ]] || fail "GitHub P4 root asset digest/size mismatch"
body="$(jq -cn '{draft:false}')"
curl --fail-with-body -sS "${auth[@]}" -H 'Content-Type: application/json' -X PATCH -d "$body" "$api/releases/$id" >/dev/null
echo "GitHub release promoter: PASS ($tag p4_sha256=$root_sha)"
