# ADR-0128 — Robokassa merchant refund runtime

Status: Accepted

## Context

The Robokassa adapter already had a provider-neutral refund method and
candidate tests, but the built-in transport returned `unsupported`. Current
official documentation now exposes a merchant Refund API. It requires an
operation `OpKey` obtained from OpStateExt or Result2, a separately generated
Password3 and a compact HS256 JWT. Its response is an asynchronous refund
request, not proof that funds have already been returned.

## Decision

Compose Robokassa refunds in the existing payment API. Before the refund POST,
the host reads OpStateExt using Password2, requires state code `100` and
extracts `Info.OpKey`. It signs `{OpKey}` for a full refund or
`{OpKey,RefundSum}` for a partial refund with HS256 and Password3, sends the
JWT as a JSON string to `/RefundService/Refund/Create`, and accepts only a
successful response with a non-empty `requestId`. The provider request ID is
returned as `RemoteRefundID` with status `accepted`.

The encrypted callback-scoped Robokassa secret remains one logical secret:
`login\npassword1\npassword2[\npassword3]`. Existing three-line accounts keep
payment/status/reconciliation/webhook behavior; refund execution requires the
fourth line. Fiscal `InvoiceItems` are not invented because the shared refund
request lacks receipt lines.

## Consequences

`payments.refund` is now present in runtime support and available to the
existing permission/policy/API path. A refund request is durable and
asynchronous, so completion must be observed by a later provider-state or
reconciliation bridge. The provider's lack of a refund idempotency field is
contained by the existing API behavior: an uncertain POST transitions to
`unknown` and is not retried blindly.

## Security and privacy impact

Password3 and all other secret lines remain callback-scoped and are never
placed in payloads, logs, events or error messages. OpKey and requestId are
bounded remote identifiers. The fixed HTTPS host and existing outbound
transport limits are reused.

## Compatibility impact

The provider-neutral SDK shape and public payment API are unchanged. The
support contract adds one already-manifested capability. Three-line credentials
remain accepted for non-refund operations.

## Operational impact

Refund requires an active production merchant account with Password3 enabled.
The live qualification fixture must cover full/partial requests, failed
provider responses, missing OpKey, non-successful payments and timeout
recovery.
