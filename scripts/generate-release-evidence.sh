#!/usr/bin/env bash
set -euo pipefail

umask 077
export LC_ALL=C
export TZ=UTC

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
inventory="$repo_root/supply-chain/release-artifacts.json"
tool_manifest="$repo_root/supply-chain/tool-versions.json"
risk_exceptions="$repo_root/supply-chain/risk-exceptions.json"
license_policy="$repo_root/supply-chain/license-policy.json"
mode="dry-run"
release_version=""
source_revision=""
repository=""
release_ref=""
workflow=""
workflow_run_id=""
workflow_run_url=""
artifact_dir=""
report_dir=""
evidence_dir=""
external_verification=""
github_attestation=""

usage() {
  echo "usage: $0 --mode dry-run|public --version SEMVER --source-revision 40_HEX --repository OWNER/NAME --ref refs/tags/vSEMVER --workflow PATH --workflow-run-id ID --workflow-run-url URL --artifact-dir DIR --report-dir DIR --evidence-dir EMPTY_DIR [--github-attestation JSON] [--external-verification JSON]" >&2
}

die() {
  echo "generate-release-evidence: $*" >&2
  exit 1
}

while (($# > 0)); do
  case "$1" in
    --mode) mode=${2:-}; shift 2 ;;
    --version) release_version=${2:-}; shift 2 ;;
    --source-revision) source_revision=${2:-}; shift 2 ;;
    --repository) repository=${2:-}; shift 2 ;;
    --ref) release_ref=${2:-}; shift 2 ;;
    --workflow) workflow=${2:-}; shift 2 ;;
    --workflow-run-id) workflow_run_id=${2:-}; shift 2 ;;
    --workflow-run-url) workflow_run_url=${2:-}; shift 2 ;;
    --artifact-dir) artifact_dir=${2:-}; shift 2 ;;
    --report-dir) report_dir=${2:-}; shift 2 ;;
    --evidence-dir) evidence_dir=${2:-}; shift 2 ;;
    --external-verification) external_verification=${2:-}; shift 2 ;;
    --github-attestation) github_attestation=${2:-}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) usage; exit 2 ;;
  esac
done

[[ "$mode" == dry-run || "$mode" == public ]] || die "--mode must be dry-run or public"
"$repo_root/scripts/check-semver.sh" "$release_version" >/dev/null || \
  die "--version must be a canonical SemVer without a v prefix"
[[ "$source_revision" =~ ^[0-9a-f]{40}$ ]] || die "invalid source revision"
[[ "$repository" =~ ^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$ ]] || \
  die "invalid repository"
[[ "$release_ref" == "refs/tags/v$release_version" ]] || die "--ref must match the release version"
[[ "$workflow" =~ ^\.github/workflows/[A-Za-z0-9._-]+\.ya?ml$ ]] || die "invalid workflow path"
[[ "$workflow_run_id" =~ ^[1-9][0-9]{0,19}$ ]] || die "invalid workflow run ID"
[[ "$workflow_run_url" == "https://github.com/$repository/actions/runs/$workflow_run_id" ]] || \
  die "workflow run URL does not match repository and run ID"

for directory in "$artifact_dir" "$report_dir" "$evidence_dir"; do
  [[ "$directory" == /* && -d "$directory" && ! -L "$directory" ]] || \
    die "artifact, report, and evidence directories must be existing absolute non-symlink directories"
done
safe_root=${TORGNEXA_SAFE_OUTPUT_ROOT:-${RUNNER_TEMP:-/tmp}}
[[ "$safe_root" == /* && -d "$safe_root" && ! -L "$safe_root" ]] || die "invalid safe output root"
safe_root="$(realpath -e -- "$safe_root")"
artifact_dir="$(realpath -e -- "$artifact_dir")"
report_dir="$(realpath -e -- "$report_dir")"
evidence_dir="$(realpath -e -- "$evidence_dir")"
for directory in "$artifact_dir" "$report_dir" "$evidence_dir"; do
  [[ "$directory" == "$safe_root/"* ]] || die "all input/output directories must be children of $safe_root"
done
[[ -z "$(find "$evidence_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]] || die "--evidence-dir must be empty"

for command_name in find install jq mktemp realpath sha256sum sort; do
  command -v "$command_name" >/dev/null 2>&1 || die "required command not found: $command_name"
done

[[ -f "$artifact_dir/SHA256SUMS" && ! -L "$artifact_dir/SHA256SUMS" ]] || die "artifact SHA256SUMS is missing"
[[ -f "$artifact_dir/SBOM_SHA256SUMS" && ! -L "$artifact_dir/SBOM_SHA256SUMS" ]] || die "SBOM_SHA256SUMS is missing"
(
  cd -- "$artifact_dir"
  sha256sum --check --strict SHA256SUMS
  sha256sum --check --strict SBOM_SHA256SUMS
) >/dev/null

context="$artifact_dir/build-context.json"
jq -e \
  --arg version "$release_version" \
  --arg revision "$source_revision" \
  --arg mode "$mode" '
    .release_version == $version and
    .source_revision == $revision and
    .mode == $mode and
    .comparator_published == false and
    (if $mode == "public" then .public_ready == true and .source_verified == true else .public_ready == false end)
  ' "$context" >/dev/null || die "build context does not match requested evidence"

jq -e '.status == "passed"' "$report_dir/source-gate.json" >/dev/null || die "source gate did not pass"
jq -e '.status == "passed"' "$report_dir/image-gate.json" >/dev/null || die "image gate did not pass"
jq -e --arg revision "$source_revision" '.source_revision == $revision' \
  "$report_dir/source-gate.json" "$report_dir/image-gate.json" >/dev/null || die "scan evidence revision mismatch"
jq -e '.exceptions | length == 0' "$risk_exceptions" >/dev/null || \
  die "evidence assembly does not silently apply risk exceptions"

if [[ "$mode" == public ]]; then
  jq -e '.public_release_ready == true' "$inventory" >/dev/null || die "public release readiness is false"
  [[ -f "$repo_root/LICENSE" && ! -L "$repo_root/LICENSE" && -s "$repo_root/LICENSE" ]] || \
    die "public evidence requires a reviewed root LICENSE"
  [[ "$external_verification" == /* && -f "$external_verification" && ! -L "$external_verification" ]] || \
    die "public evidence requires external verification JSON"
  [[ "$github_attestation" == /* && -f "$github_attestation" && ! -L "$github_attestation" ]] || \
    die "public evidence requires a GitHub attestation bundle"
fi
if [[ -n "$external_verification" ]]; then
  [[ "$external_verification" == /* && -f "$external_verification" && ! -L "$external_verification" ]] || \
    die "--external-verification must be an absolute regular JSON file"
fi

work_dir="$(mktemp -d "$safe_root/torgnexa-evidence-work.XXXXXX")"
cleanup() {
  if [[ -n "${work_dir:-}" && -d "$work_dir" ]]; then
    rm -rf -- "$work_dir"
  fi
}
trap cleanup EXIT HUP INT TERM

mkdir -m 0700 -- \
  "$evidence_dir/artifacts" "$evidence_dir/attestations" "$evidence_dir/provenance" "$evidence_dir/reports" \
  "$evidence_dir/sbom" "$evidence_dir/signatures" "$evidence_dir/policy"

sha_of() {
  local value
  value="$(sha256sum -- "$1")"
  echo "${value%% *}"
}

copy_regular() {
  local source=$1
  local destination=$2
  [[ -f "$source" && ! -L "$source" ]] || die "missing or unsafe evidence source: $source"
  install -m 0600 -- "$source" "$destination"
}

copy_regular "$license_policy" "$evidence_dir/policy/license-policy.json"
license_policy_sha="$(sha_of "$evidence_dir/policy/license-policy.json")"
license_policy_json="$(jq -cn --arg sha "$license_policy_sha" --arg commit "$source_revision" \
  '{path: "policy/license-policy.json", sha256: $sha, repository_path: "supply-chain/license-policy.json", source_commit: $commit}')"

source_gate="$report_dir/source-gate.json"
image_gate="$report_dir/image-gate.json"
database_uri="$(jq -er '.database.uri' "$source_gate")"
database_updated_at="$(jq -er '.database.updated_at' "$source_gate")"
database_digest="$(jq -er '.database.digest' "$source_gate")"
trivy_version="$(jq -er '.tools.trivy' "$source_gate")"
gosec_version="$(jq -er '.tools.gosec' "$source_gate")"
govuln_version="$(jq -er '.tools.govulncheck' "$source_gate")"

jq -s '{version: 1, reports: .}' \
  "$report_dir/gosec-root.json" "$report_dir/gosec-contractcheck.json" \
  >"$evidence_dir/reports/source-sast.json"
jq -s '{version: 1, reports: .}' \
  "$report_dir/govuln-root.json" "$report_dir/govuln-contractcheck.json" \
  "$report_dir/trivy-source-vulnerability.json" \
  >"$evidence_dir/reports/source-dependency.json"
copy_regular "$report_dir/trivy-source-license.json" "$evidence_dir/reports/source-license.json"
copy_regular "$report_dir/trivy-source-secret.json" "$evidence_dir/reports/source-secret.json"

jq -er '.binaries[] | . as $binary | .platforms[] | [$binary.name, .] | @tsv' \
  "$inventory" >"$work_dir/binaries.tsv"
jq -er '.development_runtime[] | . as $runtime | .platforms[] |
  [$runtime.name, $runtime.image, .] | @tsv' "$inventory" >"$work_dir/runtime.tsv"

subjects_json="$work_dir/subjects.jsonl"
sboms_json="$work_dir/sboms.jsonl"
signatures_json="$work_dir/signatures.jsonl"
provenance_json="$work_dir/provenance.jsonl"
: >"$subjects_json"
: >"$sboms_json"
: >"$signatures_json"
: >"$provenance_json"

subject_count=0
signature_count=0
while IFS=$'\t' read -r name platform; do
  subject_count=$((subject_count + 1))
  filename="${name}_${release_version}_linux_amd64"
  subject_source="$artifact_dir/$filename"
  subject_destination="$evidence_dir/artifacts/$filename"
  sbom_source="$artifact_dir/${filename}.spdx.json"
  sbom_destination="$evidence_dir/sbom/${filename}.spdx.json"
  copy_regular "$subject_source" "$subject_destination"
  chmod 0755 -- "$subject_destination"
  copy_regular "$sbom_source" "$sbom_destination"
  subject_sha="$(sha_of "$subject_destination")"
  sbom_sha="$(sha_of "$sbom_destination")"

  jq -cn --arg name "$name" --arg platform "$platform" \
    --arg path "artifacts/$filename" --arg sha "$subject_sha" \
    '{name: $name, type: "binary", platform: $platform, path: $path, sha256: $sha}' \
    >>"$subjects_json"
  jq -cn --arg subject "$name" --arg path "sbom/${filename}.spdx.json" \
    --arg sha "$sbom_sha" --arg subject_sha "$subject_sha" \
    '{subject: $subject, format: "SPDX-2.3-json", path: $path, sha256: $sha, subject_sha256: $subject_sha}' \
    >>"$sboms_json"

  provenance_path="provenance/${name}.intoto.json"
  jq -n \
    --arg name "$name" --arg sha "$subject_sha" \
    --arg repository "$repository" --arg commit "$source_revision" --arg ref "$release_ref" \
    --arg workflow "$workflow" --arg run_id "$workflow_run_id" --arg run_url "$workflow_run_url" '
    {
      _type: "https://in-toto.io/Statement/v1",
      subject: [{name: $name, digest: {sha256: $sha}}],
      predicateType: "https://slsa.dev/provenance/v1",
      predicate: {
        buildDefinition: {
          buildType: "https://github.com/torgnexa/torgnexa/release-build/v1",
          externalParameters: {
            repository: $repository,
            commit: $commit,
            ref: $ref,
            workflow: $workflow,
            workflow_run_id: $run_id,
            workflow_run_url: $run_url
          }
        },
        runDetails: {builder: {id: $run_url}}
      }
    }
  ' >"$evidence_dir/$provenance_path"
  provenance_sha="$(sha_of "$evidence_dir/$provenance_path")"
  jq -cn --arg subject "$name" --arg path "$provenance_path" \
    --arg sha "$provenance_sha" --arg subject_sha "$subject_sha" \
    '{subject: $subject, path: $path, sha256: $sha, subject_sha256: $subject_sha}' \
    >>"$provenance_json"

  signature_source="$artifact_dir/${filename}.sigstore.json"
  if [[ -e "$signature_source" || -L "$signature_source" ]]; then
    signature_count=$((signature_count + 1))
    signature_path="signatures/${name}.sigstore.json"
    copy_regular "$signature_source" "$evidence_dir/$signature_path"
    signature_sha="$(sha_of "$evidence_dir/$signature_path")"
    jq -cn --arg subject "$name" --arg path "$signature_path" \
      --arg sha "$signature_sha" --arg subject_sha "$subject_sha" \
      '{subject: $subject, path: $path, sha256: $sha, subject_sha256: $subject_sha}' \
      >>"$signatures_json"
  fi
done <"$work_dir/binaries.tsv"
[[ "$subject_count" == 4 ]] || die "expected four first-party subjects"
[[ "$signature_count" == 0 || "$signature_count" == 4 ]] || die "signature bundles must be absent or complete"
[[ "$mode" == dry-run || "$signature_count" == 4 ]] || die "public evidence requires four signature bundles"

runtime_json="$work_dir/runtime.jsonl"
reports_json="$work_dir/reports.jsonl"
: >"$runtime_json"
: >"$reports_json"
while IFS=$'\t' read -r name image platform; do
  runtime_subject="${name}@${platform}"
  stem="${name}_${platform//\//_}"
  runtime_sbom_source="$report_dir/${stem}.spdx.json"
  runtime_sbom_path="sbom/${stem}.spdx.json"
  copy_regular "$runtime_sbom_source" "$evidence_dir/$runtime_sbom_path"
  runtime_sbom_sha="$(sha_of "$evidence_dir/$runtime_sbom_path")"
  jq -cn \
    --arg runtime_subject "$runtime_subject" --arg runtime_image "$image" \
    --arg path "$runtime_sbom_path" --arg sha "$runtime_sbom_sha" '
    {
      subject: "", runtime_subject: $runtime_subject, runtime_image: $runtime_image,
      format: "SPDX-2.3-json", path: $path, sha256: $sha, subject_sha256: ""
    }' >>"$sboms_json"
  jq -s '{version: 1, reports: .}' \
    "$report_dir/${stem}.vulnerability.json" \
    "$report_dir/${stem}.license.json" \
    "$report_dir/${stem}.secret.json" \
    >"$evidence_dir/reports/${stem}-container.json"
  report_sha="$(sha_of "$evidence_dir/reports/${stem}-container.json")"
  jq -cn --arg name "$name" --arg platform "$platform" --arg image "$image" \
    '{name: $name, platform: $platform, image: $image}' >>"$runtime_json"
  jq -cn \
    --arg subject "$runtime_subject" --arg path "reports/${stem}-container.json" \
    --arg sha "$report_sha" --arg tool_version "$trivy_version" \
    --arg db_uri "$database_uri" --arg db_updated "$database_updated_at" --arg db_digest "$database_digest" '
    {
      kind: "container", subject: $subject, status: "pass", tool: "trivy",
      tool_version: $tool_version, path: $path, sha256: $sha, sanitized: true,
      database: {status: "present", uri: $db_uri, updated_at: $db_updated, digest: $db_digest, reason: ""}
    }' >>"$reports_json"
done <"$work_dir/runtime.tsv"

for report_spec in \
  "secret|source|trivy|$trivy_version|reports/source-secret.json|not_applicable" \
  "sast|source|gosec|$gosec_version|reports/source-sast.json|not_applicable" \
  "dependency|source|govulncheck+trivy|${govuln_version}+${trivy_version}|reports/source-dependency.json|present" \
  "license|source|trivy|$trivy_version|reports/source-license.json|not_applicable"; do
  IFS='|' read -r kind subject tool tool_version path database_status <<<"$report_spec"
  report_sha="$(sha_of "$evidence_dir/$path")"
  if [[ "$database_status" == present ]]; then
    database_json="$(jq -cn --arg uri "$database_uri" --arg updated "$database_updated_at" --arg digest "$database_digest" \
      '{status: "present", uri: $uri, updated_at: $updated, digest: $digest, reason: ""}')"
  else
    database_json='{"status":"not_applicable","uri":"","updated_at":"","digest":"","reason":"This scanner does not use a vulnerability database."}'
  fi
  jq -cn --arg kind "$kind" --arg subject "$subject" --arg tool "$tool" \
    --arg tool_version "$tool_version" --arg path "$path" --arg sha "$report_sha" \
    --argjson database "$database_json" '
    {
      kind: $kind, subject: $subject, status: "pass", tool: $tool,
      tool_version: $tool_version, path: $path, sha256: $sha, sanitized: true,
      database: $database
    }' >>"$reports_json"
done

license_json=null
external_json='{ "status": "pending", "tool": "", "tool_version": "", "path": "", "sha256": "" }'
blockers_json='[
  {"code":"external_identity_verification_pending","detail":"External OIDC identity verification is pending for this dry-run."},
  {"code":"public_release_not_ready","detail":"This evidence bundle is a non-public release rehearsal."}
]'
public_ready=false
github_attestation_json=null
if [[ -n "$github_attestation" ]]; then
  [[ "$github_attestation" == /* && -f "$github_attestation" && ! -L "$github_attestation" ]] || \
    die "--github-attestation must be an absolute regular JSON file"
  copy_regular "$github_attestation" "$evidence_dir/attestations/github-attestation.json"
  github_attestation_sha="$(sha_of "$evidence_dir/attestations/github-attestation.json")"
  github_attestation_json="$(jq -cn --arg sha "$github_attestation_sha" \
    '{path: "attestations/github-attestation.json", sha256: $sha}')"
fi
if [[ "$mode" == public ]]; then
  mkdir -m 0700 -- "$evidence_dir/repository" "$evidence_dir/verification"
  copy_regular "$repo_root/LICENSE" "$evidence_dir/repository/LICENSE"
  license_sha="$(sha_of "$evidence_dir/repository/LICENSE")"
  license_json="$(jq -cn --arg sha "$license_sha" --arg commit "$source_revision" \
    '{path: "repository/LICENSE", sha256: $sha, repository_path: "LICENSE", source_commit: $commit}')"
  blockers_json='[]'
  public_ready=true
fi
if [[ -n "$external_verification" ]]; then
  mkdir -p -m 0700 -- "$evidence_dir/verification"
  copy_regular "$external_verification" "$evidence_dir/verification/external.json"
  external_sha="$(sha_of "$evidence_dir/verification/external.json")"
  cosign_version="$(jq -er '.binary_tools[] | select(.name == "cosign") | .version' "$tool_manifest")"
  external_json="$(jq -cn --arg version "$cosign_version" --arg sha "$external_sha" \
    '{status: "pass", tool: "cosign", tool_version: $version, path: "verification/external.json", sha256: $sha}')"
  if [[ "$mode" == dry-run ]]; then
    blockers_json='[
      {"code":"public_release_not_ready","detail":"This evidence bundle is a non-public release rehearsal."}
    ]'
  fi
fi

jq -s '.' "$subjects_json" >"$work_dir/subjects.json"
jq -s '.' "$runtime_json" >"$work_dir/runtime.json"
jq -s '.' "$sboms_json" >"$work_dir/sboms.json"
jq -s '.' "$reports_json" >"$work_dir/reports.json"
jq -s '.' "$signatures_json" >"$work_dir/signatures.json"
jq -s '.' "$provenance_json" >"$work_dir/provenance.json"

jq -n \
  --arg version "v$release_version" --arg commit "$source_revision" \
  --arg repository "$repository" --arg ref "$release_ref" --arg workflow "$workflow" \
  --arg run_id "$workflow_run_id" --arg run_url "$workflow_run_url" \
  --argjson public_ready "$public_ready" \
  --slurpfile subjects "$work_dir/subjects.json" \
  --slurpfile runtimes "$work_dir/runtime.json" \
  --slurpfile sboms "$work_dir/sboms.json" \
  --slurpfile reports "$work_dir/reports.json" \
  --slurpfile signatures "$work_dir/signatures.json" \
  --slurpfile provenance "$work_dir/provenance.json" \
  --argjson license "$license_json" \
  --argjson external "$external_json" \
  --argjson github_attestation "$github_attestation_json" \
  --argjson dependency_license_policy "$license_policy_json" \
  --argjson blockers "$blockers_json" '
  {
    version: 1,
    release: {
      version: $version, commit: $commit, repository: $repository, ref: $ref,
      workflow: $workflow, workflow_run_id: $run_id, workflow_run_url: $run_url,
      public_ready: $public_ready
    },
    subjects: $subjects[0],
    runtime_subjects: $runtimes[0],
    sboms: $sboms[0],
    reports: $reports[0],
    signatures: $signatures[0],
    provenance: $provenance[0],
    github_attestation: $github_attestation,
    exceptions: [],
    dependency_license_policy: $dependency_license_policy,
    license: $license,
    external_identity_verification: $external,
    blockers: $blockers
  }' >"$evidence_dir/evidence.json"

go -C "$repo_root/tools/contractcheck" run ./cmd/releasecheck \
  -root "$evidence_dir" -manifest evidence.json -mode "$mode"

echo "self-contained $mode evidence generated in $evidence_dir"
