# Task 138 — Background payment reconciliation worker

Status: Repository-complete (external payment-gateway qualification remains environment-specific)

## Problem

`sdk.PaymentReconciler` is implemented and unit tested for `yookassa` and
`sbp` (`internal/platform/builtinruntime/paymentstransport.go`) but nothing
calls it. Today, a payment can only refresh when its own `GET
/payments/{id}` is called (`refreshPaymentStatus` in
`internal/app/api/payments.go`) — a payment nobody happens to re-read stays
stale forever. This is independent of Tasks 136/137: reconciliation is a
periodic bulk sweep and remains valuable even after webhooks exist, as the
safety net for a delivery that never arrives or fails verification.

## Scope

- A worker loop mirroring `internal/app/worker/social_publication.go`'s
  claim/lease/complete shape, but per-*account* rather than per-item: for
  each active `payment` family connector account, call
  `registry.PaymentGateway(...).ReconcilePayments` for a bounded recent
  window and reconcile the returned `sdk.PaymentSettlement` list against
  local `payments`/`payment_refunds` rows.
- Reuse `payments.ValidatePaymentTransition` and the existing
  `ChangePaymentStatus`/`ChangeRefundStatus` repository methods — do not add
  a second status-mutation path.
- A `worker_runtime_jobs` kind (extending the existing
  `claim_worker_runtime_jobs`/`release_worker_runtime_job` SQL functions the
  same way migration 000017 added `social_publication`) or, if simpler given
  reconciliation is inherently per-account rather than per-item, a plain
  polling loop over active payment accounts on a fixed interval — pick
  whichever matches this codebase's existing precedent more closely and
  justify the choice in the PR description.
- Bounded reconciliation window and pace: this must not become an unbounded
  full-history replay against the provider on every tick.

## Acceptance criteria

- A payment whose remote status changed since the last known state is
  reconciled to the correct canonical status within one worker cycle,
  without requiring a client to call `GET /payments/{id}`.
- Reconciliation never applies a transition `payments.ValidatePaymentTransition`
  rejects; an invalid/ambiguous provider settlement is logged and skipped,
  not forced.
- A provider outage during reconciliation degrades that one account's
  freshness only — it must not block or slow reconciliation of other
  accounts/tenants in the same cycle.
- Reconciliation runs are idempotent: running the same window twice produces
  no duplicate audit/outbox entries beyond what a real state change would
  produce once.
- Go test/vet, contracts, architecture and migration-static (if a new job
  kind is added) gates pass.

## Implemented repository scope

- `worker.payment-reconciliation` runs as an independent bounded component when
  worker reconciliation is enabled. It discovers tenant scopes through the
  dedicated `list_worker_payment_scopes` function, re-enters each tenant scope,
  and processes active payment accounts independently.
- The sweep is limited to the last 48 hours and runs no more often than every
  five minutes. Provider outages or invalid observations are isolated to the
  affected account and do not stop the other accounts.
- `PaymentSettlement` now carries the provider status and is validated before
  it reaches Payment Core. A local payment or refund is changed only after
  matching the connector account, remote ID, exact minor-unit amount and
  currency; lifecycle transitions still pass through the existing
  `ValidatePaymentTransition`/`ValidateRefundTransition` and corresponding
  `Change*Status` repositories. YooKassa reconciliation reads both its payment
  and refund lists; other payment rails remain limited to the settlement kinds
  they expose in their audited list contract.
- Unknown statuses, unsupported settlement kinds, amount mismatches and stale
  optimistic versions are skipped with structured worker evidence. Re-running
  the same window is idempotent because an already-applied canonical status is
  not written again.

## Explicit exclusions

- No new user-facing API or frontend surface — this is purely a freshness
  mechanism behind the existing `GET /payments` endpoints.
- SBP reconciliation remains "code-complete, unverified" for the same reason
  as its other operations (no real acquiring-bank gateway in this
  environment).
- No automatic retry of a failed *create*/*refund* — ADR-0071 already
  rejected blind retry of an ambiguous write; reconciliation only reconciles
  *status*, it never re-issues a create or refund call.
