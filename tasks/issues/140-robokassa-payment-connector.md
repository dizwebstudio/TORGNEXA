# Task 140 — Robokassa payment connector

Status: Repository implementation complete

## Problem

The Integrations surface has two payment connectors (SBP, YooKassa) but is
missing Robokassa, one of the most widely used Russian payment gateways.
Merchants who already hold a Robokassa contract cannot connect it.

## Scope

- New `connectors/payments/robokassa` package (manifest, connector, operations,
  candidate transport, conformance candidate, unit tests), mirroring the
  `connectors/payments/yookassa` structure: fixed hosts, no per-account
  `ConfigurationSource` needed.
- Real HTTP transport (`robokassaHTTP` in
  `internal/platform/builtinruntime/paymentstransport.go`):
  - `CreatePayment` via the JWT-signed (HMAC-MD5) CreateInvoice REST API.
    InvId (Robokassa's merchant-assigned invoice id) is derived
    deterministically from `ExternalID` via FNV-1a, since Robokassa's JWT
    flow has no auto-assigned invoice id.
  - `ReadPaymentStatus` / webhook re-verification via the legacy XML
    OpStateExt API.
  - `ReconcilePayments` via the JSON GetInvoiceInformationList API.
  - `RefundPayment` fails closed with a normalized `unsupported` remote
    error and no network call: Robokassa has no merchant-level refund API,
    only a Partner/aggregator `RefundOperation` requiring a distinct
    business relationship.
  - `VerifyPaymentWebhook` verifies the ResultURL's MD5 signature
    (`OutSum:InvId:Password2`) and then re-fetches state via OpStateExt
    before accepting any status change, matching the same
    never-trust-the-callback-body rule ADR-0071/ADR-0105 already apply to
    SBP/YooKassa.
- `sdk.PaymentWebhook` gains an `Ack` field so a provider's own transport
  can carry a non-generic acknowledgment body (Robokassa's ResultURL
  contract requires the literal response `"OK"+InvId` or it retries
  indefinitely) without the HTTP webhook route branching on connector
  identity — `internal/app/api/payment_webhooks.go` stays provider-agnostic
  and just echoes `verified.Ack` when set.
- Registry wiring (`internal/platform/builtinruntime/registry.go`),
  contracts entry (`contracts/connectors/builtin-runtime-support-v1.json`,
  `stage: separate_surface`, `surface: finance`, operational capabilities
  excluding `payments.refund`), frontend `presentation.json` + logo, and
  architecture governance registration (`architecture/policy.json`,
  `architecture/reviews/140-robokassa-payment-connector.json`,
  `internal/core/payments` and `internal/platform/postgres/paymentsrepo`
  modules, which predate this task but had never been registered).

## Acceptance criteria

- `go build/vet/test ./...` passes repository-wide.
- Frontend typecheck, `node --test`, and the connector-catalog generator
  pass.
- Robokassa appears in the Integrations/Finance surface with correct brand
  colors and reuses the existing `/payments` API and
  `/api/v1/webhooks/payments/{connector_id}/...` webhook route with no
  connector-specific branching in the HTTP layer.

## Explicit exclusions

- Refunds: not implemented, by design (see Scope) — Robokassa has no
  merchant-level refund API.
- Real bank/merchant qualification: unverifiable without a live Robokassa
  merchant contract, same limitation already documented for SBP.
- The Task-064 conformance suite's `sandbox_isolation` check could not be
  exercised in this repository's execution environment because it cannot
  create unprivileged Linux user namespaces (`unshare --user` returns
  `Operation not permitted`); this is an environment constraint shared by
  every connector's Task-029 sandbox probe, not a Robokassa-specific gap.
  `docs/connectors/robokassa/conformance-report.json` records the genuine
  12/13 result rather than a fabricated pass.
