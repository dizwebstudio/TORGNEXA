#!/usr/bin/env bash
set -euo pipefail
umask 077
export LC_ALL=C TZ=UTC GOTELEMETRY=off GOTOOLCHAIN=local GOWORK=off
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
evidence="" tools="" output=""
while (($#)); do
  case "$1" in
    --evidence-dir) evidence=${2:-}; shift 2;;
    --tools-dir) tools=${2:-}; shift 2;;
    --output) output=${2:-}; shift 2;;
    *) echo "usage: $0 --evidence-dir DIR --tools-dir DIR --output FILE" >&2; exit 2;;
  esac
done
fail(){ echo "external release verification: $*" >&2; exit 1; }
for x in "$evidence" "$tools"; do [[ "$x" == /* && -d "$x" && ! -L "$x" ]] || fail "absolute non-symlink directories required"; done
[[ "$output" == /* ]] || fail "absolute output path required"
for cmd in go jq sha256sum; do command -v "$cmd" >/dev/null || fail "$cmd is required"; done
[[ "$(go env GOVERSION)" == go1.26.5 ]] || fail "Go 1.26.5 is required"
cosign="$tools/cosign"; [[ -x "$cosign" && ! -L "$cosign" ]] || fail "verified cosign binary is required"
manifest="$evidence/evidence.json"; [[ -f "$manifest" && ! -L "$manifest" ]] || fail "evidence.json missing"
go -C "$root/tools/contractcheck" run ./cmd/releasecheck -root "$evidence" -manifest evidence.json -mode public >/dev/null
version="$(jq -er '.release.version' "$manifest")"; commit="$(jq -er '.release.commit' "$manifest")"; repository="$(jq -er '.release.repository' "$manifest")"; ref="$(jq -er '.release.ref' "$manifest")"; workflow="$(jq -er '.release.workflow' "$manifest")"
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]] || fail "invalid release version"
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || fail "invalid release commit"
[[ "$ref" == "refs/tags/$version" && "$workflow" == .github/workflows/release.yml ]] || fail "release identity mismatch"
jq -e '.release.public_ready == true and (.blockers|length)==0 and .external_identity_verification.status=="pass"' "$manifest" >/dev/null || fail "public evidence is blocked"
external="$evidence/verification/external.json"; [[ -f "$external" && ! -L "$external" ]] || fail "external identity evidence missing"
event_name="$(jq -er '.event_name' "$external")"; [[ "$event_name" == push ]] || fail "public release evidence must originate from tag push"
issuer=https://token.actions.githubusercontent.com
identity="https://github.com/$repository/.github/workflows/release.yml@$ref"
attestation="$evidence/attestations/github-attestation.json"; [[ -f "$attestation" && ! -L "$attestation" ]] || fail "GitHub attestation missing"
verified=0
while IFS=$'\t' read -r name path; do
  [[ "$name" =~ ^[a-z][a-z0-9_-]*$ && "$path" =~ ^artifacts/[A-Za-z0-9._-]+$ ]] || fail "unsafe binary subject entry"
  subject="$evidence/$path"; bundle="$evidence/signatures/$name.sigstore.json"
  [[ -f "$subject" && ! -L "$subject" && -f "$bundle" && ! -L "$bundle" ]] || fail "subject/signature missing for $name"
  "$cosign" verify-blob --bundle "$bundle" --certificate-identity "$identity" --certificate-oidc-issuer "$issuer" --certificate-github-workflow-repository "$repository" --certificate-github-workflow-ref "$ref" --certificate-github-workflow-sha "$commit" --certificate-github-workflow-trigger "$event_name" "$subject" >/dev/null
  "$cosign" verify-blob-attestation --bundle "$attestation" --type slsaprovenance1 --certificate-identity "$identity" --certificate-oidc-issuer "$issuer" --certificate-github-workflow-repository "$repository" --certificate-github-workflow-ref "$ref" --certificate-github-workflow-sha "$commit" --certificate-github-workflow-trigger "$event_name" "$subject" >/dev/null
  verified=$((verified+1))
done < <(jq -r '.subjects[] | select(.type=="binary") | [.name,.path] | @tsv' "$manifest")
[[ "$verified" -ge 4 ]] || fail "expected at least four verified binaries"
mkdir -p -- "$(dirname -- "$output")"
manifest_sha="$(sha256sum -- "$manifest")"; manifest_sha=${manifest_sha%% *}
jq -n --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg repository "$repository" --arg version "$version" --arg commit "$commit" --arg ref "$ref" --arg sha "$manifest_sha" --argjson subjects "$verified" '{schema_version:1,status:"PASS",verified_at:$at,repository:$repository,version:$version,commit:$commit,ref:$ref,evidence_sha256:$sha,verified_binary_subjects:$subjects}' >"$output"
chmod 0600 "$output"
echo "external release verification: PASS"
