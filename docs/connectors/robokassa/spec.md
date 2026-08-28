# Robokassa Connector Spec

Family: `payment`. JWT-signed CreateInvoice REST API for payment creation, legacy XML OpStateExt for status/webhook re-verification, MD5-signed ResultURL for webhook delivery.

Production networking is host-injected through a typed transport; provider code has no direct Core or SQL authority. InvId (Robokassa's merchant-assigned invoice id) is derived deterministically from ExternalID via FNV-1a, since Robokassa's JWT flow has no auto-assigned invoice id. Robokassa exposes no merchant-level refund API (only a Partner/aggregator `RefundOperation` requiring a distinct business relationship), so `RefundPayment` fails closed with a normalized `unsupported` remote error rather than faking success. The ResultURL webhook callback requires the literal response body `"OK"+InvId`, not a generic acknowledgment; this is carried through `sdk.PaymentWebhook.Ack` so the generic webhook route stays provider-agnostic.

Official documentation: https://docs.robokassa.ru/ ; source-verified against https://github.com/robokassa/sdk-php
