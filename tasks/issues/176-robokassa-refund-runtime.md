# Task 176: Robokassa merchant refund runtime

## Status

`repository-complete` — 2026-08-31.

## Objective

Close the Robokassa `payments.refund` runtime gap using the current official
merchant Refund API while preserving the existing provider-neutral payment
lifecycle and ambiguous-write handling.

## Deliverables

- read `Info.OpKey` from the authenticated OpStateExt response;
- require a successfully paid operation before refund egress;
- sign the compact Refund API JWT with HS256 and Password3;
- support exact full and partial RUB refunds;
- return the asynchronous provider `requestId` as `accepted`;
- extend the callback-scoped secret format with optional Password3 while
  preserving three-line payment-only compatibility;
- synchronize runtime support, frontend credential guidance, matrix, docs,
  tests and qualification evidence.

## Scope limits

The runtime does not send fiscal `InvoiceItems` because the current
provider-neutral refund request has no receipt-line contract. A merchant that
requires a fiscal refund receipt needs a later additive contract extension.
Refund API timeouts remain `unknown` in the existing API layer and are never
blindly retried. Live merchant credentials, Password3 activation and provider
qualification remain release-topology gates.

## Verification

Run `gofmt`, `go test ./...`, `go vet ./...`,
`./scripts/check-contracts.sh`, the frontend generated-catalog checks and
`git diff --check`.
