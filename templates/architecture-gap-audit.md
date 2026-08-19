# Architecture Gap Audit: <task/change>

- Change class: implementation / new domain / new provider / pillar / mixed
- Exact protected paths:
- Frozen pillars affected:
- Existing ADRs:
- New/superseding ADR (required for pillar/gate/mixed):
- Decision: within frozen architecture / extend generic capability / Connector SDK / architecture change

For each item record `affected`, `not_affected`, or `not_applicable` plus a
meaningful rationale/evidence path. A marker such as `N/A`, `TODO`, or `TBD`
does not satisfy the gate.

- Tenancy:
- Privacy/data governance:
- API compatibility:
- Event compatibility:
- Plugin/Connector SDK compatibility:
- Database migration/backfill:
- Security:
- Approvals/risk:
- Audit/lineage:
- Reconciliation/idempotency:
- Webhooks/egress:
- SLO/observability:
- Operations/rollback:
- Tests/conformance:

Provider-only additions also require the registered adapter root, manifest,
Connector Spec, capability audit, and conformance plan. Provider plus generic
Core/SDK work is `mixed` and requires the union of provider evidence and a new
ADR.
