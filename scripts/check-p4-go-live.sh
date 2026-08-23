#!/usr/bin/env bash
set -euo pipefail
umask 077
export LC_ALL=C TZ=UTC GOTELEMETRY=off GOTOOLCHAIN=local GOWORK=off
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"; cd "$root"
fail(){ echo "P4 go-live qualification: $*" >&2; exit 1; }
for cmd in go docker python3 jq sha256sum git; do command -v "$cmd" >/dev/null || fail "$cmd is required"; done
[[ "$(go env GOVERSION)" == go1.26.7 ]] || fail "Go 1.26.7 is required; got $(go env GOVERSION)"
docker compose version >/dev/null || fail "Docker Compose v2 is required"
[[ -z "$(git status --porcelain)" ]] || fail "qualification requires a clean Git worktree"
[[ ! -e "$root/.env" ]] || fail "source worktree must not contain .env; production credentials must stay outside the repository"
version=${TORGNEXA_P4_VERSION:-}; repository=${TORGNEXA_P4_REPOSITORY:-}; branch=${TORGNEXA_P4_PROTECTED_BRANCH:-main}; base_url=${TORGNEXA_P4_BASE_URL:-}
[[ -n "$version" && -n "$repository" && -n "$base_url" ]] || fail "TORGNEXA_P4_VERSION, TORGNEXA_P4_REPOSITORY and TORGNEXA_P4_BASE_URL are required"
./scripts/check-semver.sh "$version" >/dev/null
commit="$(git rev-parse --verify HEAD)"; [[ "$commit" =~ ^[0-9a-f]{40}$ ]] || fail "invalid HEAD"
[[ "$(git describe --tags --exact-match HEAD 2>/dev/null || true)" == "v$version" ]] || fail "HEAD must be exact tag v$version"
plan=${TORGNEXA_P4_CONNECTOR_PLAN:-}; posture=${TORGNEXA_P4_POSTURE_FILE:-}; release_evidence=${TORGNEXA_P4_RELEASE_EVIDENCE_DIR:-}; tools=${TORGNEXA_P4_SECURITY_TOOLS_DIR:-}
[[ "$plan" == /* && -f "$plan" && "$posture" == /* && -f "$posture" && "$release_evidence" == /* && -d "$release_evidence" && "$tools" == /* && -d "$tools" ]] || fail "absolute connector plan, posture, release evidence and security tools paths are required"
[[ -n "${TORGNEXA_P4_GITHUB_RELEASE_TOKEN:-}" ]] || fail "TORGNEXA_P4_GITHUB_RELEASE_TOKEN is required for staged-release digest verification"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"; out=${TORGNEXA_P4_EVIDENCE_DIR:-$root/qualification/evidence/p4-$stamp}; [[ "$out" == /* ]] || fail "evidence path must be absolute"; mkdir -p "$out"; [[ -z "$(find "$out" -mindepth 1 -maxdepth 1 -print -quit)" ]] || fail "evidence directory must be empty"
TORGNEXA_P3_EVIDENCE_DIR="$out/p3" make p3-qualification >"$out/p3.log" 2>&1
python3 scripts/p4_hosting_rules.py capture --repository "$repository" --branch "$branch" --output "$out/github-applied-rules.json"
python3 scripts/p4_hosting_rules.py verify --applied-rules "$out/github-applied-rules.json" --output "$out/github-protection.json"
python3 scripts/p4_posture.py --input "$posture" --output "$out/production-posture.json"
./scripts/verify-release-evidence-external.sh --evidence-dir "$release_evidence" --tools-dir "$tools" --output "$out/release-verification.json"
./scripts/package-release-evidence.sh --evidence-dir "$release_evidence" --output "$out/rebuilt-release-evidence.tar.gz" >/dev/null
python3 scripts/p4_release_stage.py --repository "$repository" --version "$version" --evidence-dir "$release_evidence" --bundle "$out/rebuilt-release-evidence.tar.gz" --output "$out/staged-release.json"
python3 scripts/p4_live_connectors.py --base-url "$base_url" --plan "$plan" --output "$out/connectors.json"
python3 scripts/p4_root_evidence.py compose --evidence-dir "$out" --version "$version" --commit "$commit" --repository "$repository" --output "$out/p4-go-live.json"
chmod -R go-rwx "$out"
echo "P4 go-live qualification: PASS"
echo "evidence: $out"
