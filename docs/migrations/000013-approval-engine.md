# Migration 000013 — Approval Engine

Task 017 adds a tenant-scoped, policy-versioned approval workflow without changing existing commerce tables.

Expand additions:

- immutable versioned `approval_policies` with action/resource/risk matching, staged quorum, eligible scopes, TTL and escalation metadata;
- `approval_requests` with optimistic version, expiry, escalation, execution and terminal-state timestamps;
- append-only `approval_decisions` carrying immutable approver-scope evidence;
- append-only `approval_escalations`;
- forced RLS for all approval tables;
- four-eyes, eligible-scope, quorum and lifecycle trigger enforcement;
- no-delete/no-truncate protection for workflow evidence.

Policy rows are versioned. Installing a new active policy retires the previous active action/resource policy while historical requests retain the exact policy id/version they started under.

The migration is expand-only: no existing table or contract is removed or renamed. Old readers/writers remain unaffected. New Task-017 binaries require the new approval tables only when approval APIs/workflows are invoked.
