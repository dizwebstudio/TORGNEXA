# 5Post Connector Specification

## Capability

- provider id: `fivepost`
- display name: `5Post`
- family: `logistics`
- SDK: v1
- authentication: partner API key, exchanged by the host for a short-lived JWT
- runtime surface: separate «Доставка» account, credential check, bounded
  pickup-point directory read, C2C tariff preview, single-order status read and
  one-parcel create

The adapter admits only operations documented for the 5Post partner API:
shipment creation, shipment cancellation, current status lookup, label
retrieval, pickup-point directory reads and the bounded C2C tariff endpoint.
The tariff route requires explicit UUIDs for the placement and issue points;
the host never guesses them from city or address text. Webhooks and return
creation are not claimed until the current partner contract documents those
routes.

## Transport contract

The provider receives a host-injected typed `Transport`. The host is responsible
for JWT acquisition, HTTPS, timeouts, retry policy, response-size limits and
artifact storage. The provider package has no direct network, filesystem,
database or environment authority.

Remote shipment and pickup-point identifiers remain inside the adapter. The
host maps them to canonical shipment/PUDO identities; Core never branches on
the provider name. The current application exposes the credential probe, one
bounded pickup directory page, C2C tariff preview, one-order status lookup,
cancellation by provider order UUID, one PDF label read and a one-parcel
universal order create. Create requires explicit non-secret sender-location,
return-policy and barcode-enrichment configuration plus declared product lines.
Tariff prices are normalized from exact decimal JSON into RUB minor units and
delivery days are bounded before returning the neutral quote.

## Authentication and privacy

API keys and JWTs are callback-scoped secrets. The connector emits only bounded
normalized shipment/status/label metadata and never copies recipient PII or raw
provider payloads into events, logs or durable Core records.
