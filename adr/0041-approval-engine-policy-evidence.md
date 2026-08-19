# ADR 0041: Versioned approval policy and immutable decision evidence

Status: Accepted

## Context

Sensitive and legally significant TORGNEXA writes must not rely on UI confirmation, connector-specific flags, mutable role checks, or a best-effort audit trail. The authorization decision must be reproducible after policy changes, survive retries, remain tenant-scoped, and provide evidence that quorum and separation-of-duties were actually satisfied.

## Decision

Task 017 implements a provider-neutral approval engine in `internal/platform/approval` with PostgreSQL persistence in `internal/platform/postgres/approvalrepo`.

Policies are immutable versions bound to tenant/workspace, action, resource type, minimum risk, ordered stages, eligible scopes, quorum, TTL, escalation interval, and separation-of-duties. A request snapshots the exact policy id/version. Decisions and escalation evidence are append-only. PostgreSQL forced RLS and triggers enforce four-eyes, scope eligibility, quorum, and lifecycle progression in addition to application validation.

Sensitive and legally-significant operations fail closed when no matching active policy exists. Approval authorizes only the exact action/resource identity and remains distinct from downstream execution success. Execution starts only from `approved` before expiry and ends `completed` or `failed` with a bounded machine reason code.

Every workflow mutation shares one PostgreSQL transaction with append-only audit and Transactional Outbox intent. Task 030 later adds cross-domain lineage without changing this authorization boundary.

## Consequences

Policy edits create a new version instead of rewriting history. Requester self-approval and quorum bypass are blocked at both application and database layers. IAM remains authoritative for trusted scope issuance while the approval decision snapshots the scopes actually used. Connector, AI, n8n, and MCP code cannot define a provider-local bypass. Downstream modules can reference an approval request id as durable authorization evidence.

## Alternatives considered

A single boolean `approved` column was rejected because it cannot prove who approved, under which policy version, or whether quorum was satisfied. Provider-specific approval flags were rejected because they would leak external-system semantics into Core. Mutable policy rows were rejected because historical decisions would become non-reproducible. UI-only confirmation and audit-only logging were rejected because neither prevents unauthorized execution.

## Compatibility impact

The change is additive. Existing APIs and Connector SDK v1 interfaces are unchanged. New Draft 2020-12 approval policy/request/decision contracts and additive `governance.approval.*.v1` event payloads are introduced without modifying previously published event schemas.

## Migration and data impact

Expand migration `000013_approval_engine.sql` adds `approval_policies`, `approval_requests`, `approval_decisions`, and `approval_escalations`. No existing table or column is dropped or renamed. Policy versions and decision/escalation evidence are retained as historical authorization records; emergency binary rollback must not delete those tables.

## Security and privacy impact

Sensitive and legally-significant actions deny when no matching policy exists. Four-eyes, stage scopes, quorum, exact policy version, expiry, and legal lifecycle transitions are enforced in both Go and PostgreSQL. Decision comments are bounded and secret-shaped text is redacted. Approval events contain identifiers, state, risk, stage, and machine codes only; raw provider bodies, credentials, and arbitrary failure strings are excluded.

## Operational impact

Operators gain measurable pending age, expiry, escalation count, approval latency, and execution outcome states. Policy changes are versioned and old requests continue under their original version. Schedulers may use the due-request query for expiry/escalation processing. Task 030 may later expose a consolidated timeline, while Task 084 will supply enterprise-federated actor scopes into the same approval contract.
