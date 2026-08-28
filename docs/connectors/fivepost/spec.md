# 5Post Connector Specification

## Capability

- provider id: `fivepost`
- display name: `5Post`
- family: `logistics`
- SDK: v1
- authentication: partner API key, exchanged by the host for a short-lived JWT
- runtime surface: separate «Доставка» account and credential check

The adapter admits only operations documented for the 5Post partner API:
shipment creation, shipment cancellation, current status lookup, label
retrieval and pickup-point directory reads. The tariff calculation is not
advertised as `logistics.rates.read`: 5Post pricing is contract- and tariff-zone
dependent and cannot be calculated from the provider-neutral rate request
without additional reviewed runtime configuration. Webhooks and return
creation are also not claimed until the current partner contract documents
those routes.

## Transport contract

The provider receives a host-injected typed `Transport`. The host is responsible
for JWT acquisition, HTTPS, timeouts, retry policy, response-size limits and
artifact storage. The provider package has no direct network, filesystem,
database or environment authority.

Remote shipment and pickup-point identifiers remain inside the adapter. The
host maps them to canonical shipment/PUDO identities; Core never branches on
the provider name. The current application exposes only the credential probe;
shipment calls remain qualification-gated until the partner API fixtures are
reviewed.

## Authentication and privacy

API keys and JWTs are callback-scoped secrets. The connector emits only bounded
normalized shipment/status/label metadata and never copies recipient PII or raw
provider payloads into events, logs or durable Core records.
