# Migration 000026 — AI Agent Governance

## Purpose

Add durable tenant-scoped storage for immutable AI-agent policy, kill-switch history and replica-safe frequency enforcement required by Task `079`.

## Phase / risk

- phase: `expand`
- risk: `high`
- transaction: embedded
- backfill: none
- backup required: yes

The migration is additive; existing readers/writers do not depend on these tables.

## Tables

- `ai_agent_policies`: stable policy id plus monotonically increasing version per tenant/agent/integration. Rules are bounded JSON, effective windows are explicit, and trusted actor/reason/creation time are retained.
- `ai_agent_kill_switches`: append-only monotonically versioned tenant/agent/integration disabled state with actor, reason and timestamp.
- `ai_agent_call_counters`: mutable only in the monotonic `used` dimension; identity/window/max snapshot cannot change.
- `ai_agent_call_usage`: append-only per-invocation allow/deny receipt used for retry idempotency.

All tables use composite workspace foreign keys, explicit tenant predicates, `ENABLE ROW LEVEL SECURITY` plus `FORCE ROW LEVEL SECURITY`.

## Deployment

1. take/verify the required database backup;
2. apply the migration with the standard migration-history session variables;
3. verify forced RLS and append-only/version guards;
4. install governance policy only through the trusted control plane;
5. keep agent/MCP execution fail-closed until policy and Task-084 identity composition are qualified.

## Rollback

Application rollback is safe because the schema is additive. Do not destructively drop governance tables during an operational rollback: policy, kill-switch and usage receipts are security evidence. A later reviewed contract migration may remove them only after retention/compliance requirements are satisfied.
