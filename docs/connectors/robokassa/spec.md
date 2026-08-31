# Robokassa Connector Spec

Family: `payment`. JWT-signed CreateInvoice REST API for payment creation,
legacy XML OpStateExt for status and webhook re-verification, MD5-signed
ResultURL delivery, and Password3-signed merchant refunds.

Production networking is host-injected through a typed transport; provider code
has no direct Core or SQL authority. InvId (Robokassa's merchant-assigned
invoice id) is derived deterministically from ExternalID via FNV-1a. Refunds
first read the authoritative OpStateExt record, require state `100` and a
non-empty `Info.OpKey`, then call
`POST https://services.robokassa.ru/RefundService/Refund/Create` with a compact
HS256 JWT signed by Password3. The provider's asynchronous `requestId` is
returned as `accepted`; a network ambiguity is never blind-retried by the API
layer. Full refunds omit `RefundSum`; partial refunds send exact RUB minor
units as a decimal number.

The ResultURL callback verifies `OutSum:InvId:Password2`, re-fetches state via
OpStateExt, and requires the literal response body `OK` plus invoice ID. This
acknowledgment travels through `sdk.PaymentWebhook.Ack`, keeping the generic
webhook route provider-agnostic.

Official documentation: https://docs.robokassa.ru/ ; source-verified against
the current [merchant refund API](https://docs.robokassa.ru//ru/refund-api),
[XML interfaces](https://docs.robokassa.ru/ru/xml-interfaces) and
[Invoice API](https://docs.robokassa.ru/ru/invoice-api).
