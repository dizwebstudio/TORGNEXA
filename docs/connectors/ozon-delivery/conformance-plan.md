# Ozon Delivery conformance plan

The adapter uses Connector SDK v1 and a host-owned transport. Tests cover
tenant/account validation, callback-scoped credential use and normalized
health failures; the production probe is bounded to the warehouse list.

Before admitting delivery operations, qualify a dedicated Ozon seller with
synthetic addresses and parcels: quote determinism, shipment idempotency,
cancel/track transitions, label artifact handling, pickup-point bounds and
provider rate-limit behavior. Never put recipient PII or production keys into
fixtures.
