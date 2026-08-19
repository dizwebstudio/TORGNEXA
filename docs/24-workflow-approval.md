# Workflow / Approval Engine

Task 017 provides the provider-neutral authorization workflow used by price/stock mass changes, advertising budget changes, destructive operations, regulated/signing actions and AI-requested writes.

## Fail-closed risk gate

Canonical risk classes are `read`, `write_safe`, `write_sensitive`, and `legally_significant`.

- read / write-safe actions may proceed without an approval policy;
- write-sensitive / legally-significant actions deny when no matching active policy exists;
- a matching policy returns `approval_required`; callers must not execute the sensitive action until the request reaches `approved` and execution is begun for the exact action/resource identity.

AI, MCP, n8n and connector code are ordinary principals and cannot bypass this gate.

## Versioned policies

An approval policy binds exactly one tenant/workspace action + resource type to:

- minimum risk class;
- immutable policy version;
- request TTL;
- optional escalation interval;
- four-eyes / separation-of-duties rule;
- one to sixteen ordered stages;
- per-stage approver scopes and quorum.

Installing a new version retires the previous active action/resource policy. Existing approval requests retain the exact `policy_id + policy_version` they started with, so historical decisions remain reproducible after policy changes.

## State machine

Persistent request states:

`pending -> approved | rejected | expired | cancelled`

`approved -> executing | expired | cancelled`

`executing -> completed | failed`

A request may remain `pending` while collecting votes or while advancing to the next stage. Optimistic `version` prevents lost updates.

Approval does not equal successful execution. The separate `executing -> completed|failed` lifecycle preserves evidence that a valid approval existed even when the downstream operation failed.

## Four-eyes, quorum and scopes

Each immutable decision contains the approver id, stage, approve/reject vote, bounded sanitized comment, UTC timestamp and a sorted snapshot of the approver scopes used for the decision.

The application and PostgreSQL both enforce:

- requester cannot approve their own request when separation-of-duties is enabled;
- approver must have at least one scope eligible for that stage;
- one actor may decide a stage only once;
- any rejection makes the request rejected;
- a stage cannot advance before quorum;
- final `approved` state requires quorum in every configured stage.

The identity/IAM layer remains authoritative for issuing actor scopes; Task 084 later adds enterprise federation/provisioning around the same scope contract.

## Expiry and escalation

Every request has an absolute UTC `expires_at`. Approved requests must begin execution before expiry; otherwise they fail closed and may be transitioned to `expired`.

Policies may define `escalate_after`. Escalation does not auto-approve or change the quorum. It increments immutable workflow evidence, updates `next_escalation_at`, emits audit/outbox evidence and leaves the request pending.

## Evidence and events

All mutations execute in one PostgreSQL transaction with append-only audit and Transactional Outbox intent.

Events:

- `governance.approval.policy_changed.v1`
- `governance.approval.requested.v1`
- `governance.approval.decided.v1`
- `governance.approval.escalated.v1`
- `governance.approval.state_changed.v1`

Event/audit payloads contain identifiers, risk, stage, state and bounded reason codes. They do not include raw secrets, provider bodies or arbitrary failure text.

## Persistence security

Migration `000013_approval_engine.sql` creates forced-RLS approval policies, requests, decisions and escalation evidence. Decision/escalation rows are append-only. Requests may only progress through the canonical lifecycle with monotonic version/stage/escalation fields. Direct SQL cannot forge final approval without immutable decision quorum.

Task 030 adds cross-domain lineage/timeline metadata on top of these approval identifiers; it does not replace Task 017 authorization.
