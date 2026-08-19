# ADR 0035: Architecture change governance

Status: Accepted

## Context

The v1 pillars and dependency direction were documented but not enforced on a
pull-request change set. A stale checklist or an already-existing ADR could be
reused while provider logic, a new domain, or a changed pillar entered Core.
The connector SDK and conformance boundary are not yet complete, so provider
admission must remain closed rather than being simulated by this task.

## Decision

Maintain a strict architecture policy and append-only structured review records.
The local tree gate validates module/provider inventory, record references,
impact completeness, the complete first-party Go source layout, and Go import
direction. Pull-request CI builds the verifier offline from the exact trusted
base SHA and executes it before pull-request code. It evaluates the complete
merge-base-to-head diff, binds each sensitive path to a new record of the
required class (and provider ID where applicable), protects accepted and cited
ADRs from mutation, and requires an accepted newly added ADR for frozen-pillar,
gate, or mixed provider/Core changes.

Provider admission remains disabled until Tasks 010, 025, 029, and 064 close.
Opening it is itself a reviewed pillar change whose prerequisite completion is
already visible in the merge base. A prerequisite completion transition must
be task-bound to its first fresh architecture review and exact issue path;
completed prerequisite issues are immutable. An admitted provider must live
under a registered connector/plugin root, use the Connector SDK route, provide
manifest/spec/capability/conformance evidence, and must not import Core, App, or
PostgreSQL internals.

Provider retirement is explicit rather than inferred from an old review. The
retirement record must be introduced by the same exact-base diff that removes a
registered provider, preserve its canonical evidence references, and name the
fresh pillar/mixed review task. The retired implementation directory may retain
only a regular non-executable manifest; code and executable plugin payloads are
removed. Retirement records are immutable. Any later retained-evidence change
remains sensitive and requires a fresh matching review, so repository history
preserves both versions and their decisions.

## Consequences

Core and Platform changes carry explicit, reviewable impact evidence. New
packages cannot silently expand the architecture inventory, accepted decisions
are superseded instead of rewritten, and provider work cannot bypass the SDK by
combining generic and provider changes in one classification. The policy adds
small review overhead and deliberately fails closed when history is shallow or
provider prerequisites are incomplete.

## Alternatives considered

A Markdown-only PR checklist was rejected because CI cannot validate its
meaning or bind it to changed files. Provider-name grep alone was rejected as
the primary control because aliases and new providers make it incomplete. A
remote policy service was rejected because local and self-hosted checks must be
deterministic and available offline.

## Compatibility impact

No REST, event, webhook, protobuf, or plugin contract changes. Existing source
packages keep their import graph. Future PRs that touch protected paths must add
the structured evidence required by the gate.

## Migration and data impact

No database or persisted-data migration is introduced. Task 067 remains the
authority for SQL catalog, expand/migrate/contract, backup, and rehearsal
requirements; architecture evidence references rather than duplicates it.

## Security and privacy impact

The policy rejects symlinks, traversal, duplicate/unknown JSON fields,
oversized inputs/inventories, unclassified Go roots, cgo/linkname escapes,
forbidden dependency directions, direct provider access to Core/database
internals, mutable accepted or cited ADRs, stale provider evidence, and
incomplete gap analysis.
Review evidence is bounded and contains rationale/paths only, never secrets or
production PII. Diagnostics are deterministic, bounded, and control-character
sanitized.

## Operational impact

`make architecture` runs offline in the normal repository gate. Pull-request
CI requires full history and exact base/head revisions; missing history or a
shallow, sparse, dirty, mismatched, or untracked checkout fails. The verifier is
built from the base before HEAD code, so a running trusted workflow does not
execute a verifier or wrapper supplied by HEAD. A pull request can still alter
or remove its own workflow unless the hosting platform independently requires a
protected workflow. That Ruleset Required Workflow (or equivalent immutable
check), branch policy, and reviewer configuration remain an external
operational qualification and must be verified on the first post-merge
protected pull request whose base already contains this decision.
