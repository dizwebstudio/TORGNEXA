# Task 137 — Payment provider webhook receivers

Status: Repository implementation complete
Depends on: Task 136 (webhook ingress boundary) — done

## Problem

`internal/app/api/payments.go` ships create/list/get/refund and closes the
freshness gap only by re-checking status on a single-payment `GET` (see the
`refreshPaymentStatus` comment there for why). Nothing calls
`sdk.PaymentWebhookVerifier`, so a completed checkout is invisible until the
customer's browser returns to a `GET /payments/{id}` call. This task adds the
actual receivers on top of Task 136's boundary; no new architecture decision
is needed beyond ADR-0105/ADR-0071, since `VerifyPaymentWebhook` for both
`yookassa` and `sbp` is already implemented and unit tested
(`internal/platform/builtinruntime/paymentstransport_test.go` —
`TestYooKassaVerifyWebhookIgnoresUnverifiedBodyStatus` already proves the body
is never trusted directly).

## Scope

- Register `POST /api/v1/webhooks/payments/{connector_id}/{account_id}`
  through Task 136's public route table.
- Resolve the connector account and tenant scope from `account_id`; reject
  (uniformly, per Task 136) if it does not resolve to an active `payment`
  family account of the claimed `connector_id`.
- Call `registry.PaymentGateway(...).VerifyPaymentWebhook` inside the
  account's own `UseSecret` scope (same pattern `dispatchCreate`/
  `dispatchRefund` already use in `payments.go`).
- Record `payments.WebhookEvidence` via the existing
  `Repository.RecordWebhookEvidence` (append-only, replay-deduped by
  `delivery_id` — already implemented in `paymentsrepo`); skip applying a
  status change when the evidence already existed (replay).
- Apply the verified status through the existing
  `payments.ValidatePaymentTransition` / `ChangePaymentStatus` path — reuse
  `paymentsCanonicalStatus` from `payments.go` rather than duplicating the
  remote-status mapping.
- Update `GET /payments/{id}` to stop needing its read-through refresh once
  webhooks are the primary freshness path (keep it as a fallback — cheap and
  still correct — rather than removing it).

## Acceptance criteria

- A verified, previously-unseen delivery moves the payment through exactly
  one valid `payments.ValidatePaymentTransition` step and records one audit
  entry and one outbox event, matching the existing `ChangePaymentStatus`
  contract in `paymentsrepo`.
- A byte-identical redelivery is a no-op (no second status-change attempt, no
  second audit/outbox entry) — proven by a test asserting exactly one
  `ChangePaymentStatus` call across two identical deliveries.
- A delivery for an unknown/inactive account, or one that fails provider
  callback verification, never reaches `ChangePaymentStatus` and produces the
  uniform Task 136 response.
- The webhook body's claimed status is never used to decide the target
  state — only the verified callback response is (extend the existing
  `TestYooKassaVerifyWebhookIgnoresUnverifiedBodyStatus`-style coverage to the
  route handler itself, not just the transport).
- OpenAPI/SDK regeneration, Go test/vet, contracts and architecture gates
  pass.

## Explicit exclusions

- SBP webhook delivery cannot be qualified end-to-end without a real
  acquiring-bank gateway (same limitation already documented for SBP's
  create/status/refund paths); Task 180 admits the repository runtime while
  retaining the "code-complete, honestly unverified" bar, not a live
  qualification claim.
- No provider-signature verification framework — out of scope per ADR-0105
  until a signing provider is actually admitted.
- No frontend changes; `FinancePage.tsx` already renders whatever status the
  API returns.
