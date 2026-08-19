# ADR 0039: Connector conformance becomes executable admission evidence

Status: Accepted

## Context

Connector SDK v1, the signed plugin permission boundary, and the dry-run/Linux reference sandbox are already implemented by Tasks 010, 025, and 029. The architecture policy can require provider manifests and conformance plans, but a prose plan does not prove auth normalization, retry behavior, duplicate safety, tenant isolation, or sandbox enforcement. Provider admission therefore still lacks executable, machine-verifiable evidence.

## Decision

Task 064 adds a reusable Connector SDK conformance runner under the approved SDK prefix, a strict machine-readable report contract, and a repository reference qualification. Every future active provider must retain a canonical passing `docs/connectors/<id>/conformance-report.json`; the architecture checker validates the report structure, digest, pass state, and connector identity. The suite has thirteen mandatory checks with no required-check skip state. Provider admission remains disabled in this task because Task 080 requires Task 064 to be completed in the merge base before the separate admission-control change can enable it.

## Consequences

Provider implementations gain one common certification surface rather than bespoke per-provider release claims. A connector that cannot demonstrate normalized auth/health/errors, bounded retry, idempotency, webhook replay handling, tenant isolation, dry-run suppression, production-secret rejection, egress denial, resource-limit failure, and reference sandbox isolation cannot be admitted. The architecture checker grows a dependency on the Connector SDK conformance report type but provider code remains limited to the existing SDK prefix.

## Alternatives considered

Keeping only prose conformance plans was rejected because plans are not executable evidence. Allowing providers to define their own report schemas was rejected because results would not be comparable and could omit security failures. Enabling provider admission in the same Task-064 change was rejected because the trusted-base architecture rule intentionally requires all admission prerequisites to be completed before they appear in the merge base.

## Compatibility impact

Connector SDK v1 root interfaces remain unchanged. Task 064 adds the `internal/platform/connectors/conformance` subpackage and one additive Draft 2020-12 report schema. Existing provider manifest, plugin security, sandbox, API, webhook, event, and database contracts are not modified.

## Migration and data impact

No database migration or durable application data change is introduced. Future provider repositories add one canonical conformance report evidence file under `docs/connectors/<id>/`. Existing environments require no backfill because provider inventory is empty while admission remains disabled.

## Security and privacy impact

Reports contain only bounded machine codes and hashes; raw provider error text, credentials, Authorization material, provider bodies, and PII are excluded. The suite verifies production-credential rejection, side-effect-free dry-run, egress grants, tenant isolation, and Linux sandbox evidence. A report digest detects post-generation mutation but is not a publisher signature and does not replace Task-025 artifact signatures.

## Operational impact

`make check` gains a conformance reference qualification. On Linux it requires the same `unshare` isolation support as Task 029; a missing Linux isolation prerequisite fails the qualification rather than silently downgrading it. Provider release pipelines must run their candidate adapter and preserve the passing report before provider admission review.
