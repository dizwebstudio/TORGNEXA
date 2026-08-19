# Task 017

## Status

Repository implementation: **Completed** on 2026-08-10.

## Objective

Implement the canonical approval state machine, scopes, expiry/escalation metadata, immutable decision evidence and audit/outbox integration for sensitive and legally significant writes.

## Deliverables

- [x] Fail-closed `allow | approval_required | deny` risk gate.
- [x] Immutable versioned policies with exact tenant/action/resource/risk binding.
- [x] Ordered multi-stage quorum and eligible approver scopes.
- [x] Four-eyes / requester-cannot-approve enforcement.
- [x] Immutable approver scope snapshot and one-vote-per-stage evidence.
- [x] Pending/approved/rejected/expired/cancelled/executing/completed/failed lifecycle.
- [x] UTC request expiry and explicit escalation metadata/evidence; escalation never auto-approves.
- [x] Optimistic request versioning.
- [x] PostgreSQL forced RLS plus DB-side four-eyes/scope/quorum/lifecycle guards.
- [x] Append-only decision/escalation evidence and no-delete/no-truncate guards.
- [x] Atomic Audit + Transactional Outbox events for policy/request/decision/escalation/execution changes.
- [x] Draft 2020-12 policy/request/decision and event contracts with positive/negative fixtures.
- [x] Migration, architecture, docs and regression checks.

## Boundaries

Task 017 authorizes a sensitive action; it does not itself perform the downstream action. Callers bind an approval request to the exact action/resource identity and begin execution only after approval and before expiry. Task 030 owns cross-domain lineage/timeline reads. Task 084 owns enterprise IAM/federated identity; its trusted scopes feed this engine rather than bypassing it.

## Acceptance

Implementation + tests + updated contracts/docs; run required checks. Sensitive/legally-significant writes fail closed without matching policy; four-eyes, quorum, scopes, expiry and immutable audit evidence are machine-enforced.
