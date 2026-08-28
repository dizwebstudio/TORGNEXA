# Ozon Pay conformance plan

The adapter uses the frozen Connector SDK v1 and a host-owned transport. Unit
tests cover callback-scoped credentials, account identity validation and
normalized health failures. The host probe is deterministic in tests and sends
only a bounded Seller API request in production.

Before enabling payment operations, qualify a dedicated Ozon Pay merchant
account with synthetic orders: create/status/refund idempotency, signed webhook
replay, settlement mapping and failure/retry semantics. Do not retain live
payment credentials or customer payment data as fixtures.
