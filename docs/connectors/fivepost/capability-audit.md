# 5Post Capability Audit

The official 5Post partner material describes API-key access with a subsequent
JWT exchange, pickup-point directory data, order creation, order cancellation,
status reconciliation and label retrieval. The public partner page provides
the current API downloads; the partner portal contract remains the authority
for exact endpoint paths and field requirements:

- https://fivepost.ru/become-partner/
- https://test-omni.x5.ru/files/public/%D0%98%D0%BD%D1%81%D1%82%D1%80%D1%83%D0%BA%D1%86%D0%B8%D1%8F_%D0%9F%D0%BE%D1%80%D1%82%D0%B0%D0%BB_%D0%BF%D0%B0%D1%80%D1%82%D0%BD%D1%91%D1%80%D0%B0_5Post.pdf

The SDK adapter therefore claims only the five capabilities that can be
normalized without guessing a tenant's tariff, warehouse, payment or PII
contract. Rates, returns, webhooks and courier pickup are explicit follow-up
qualification items, not hidden promises in the manifest.

The connector is admitted to the separate logistics surface for tenant account
configuration and an authenticated credential probe. Shipment operations
remain fail-closed until a host-side write bridge is reviewed and a
non-production 5Post partner account is available.
