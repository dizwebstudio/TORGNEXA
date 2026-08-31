# 5Post Capability Audit

The official 5Post partner material describes API-key access with a subsequent
JWT exchange, pickup-point directory data, order creation, order cancellation,
status reconciliation and label retrieval. The public partner page provides
the current API downloads; the partner portal contract remains the authority
for exact endpoint paths and field requirements:

- https://fivepost.ru/become-partner/
- https://fivepost.ru/files/public/API_5post.docx (v7.32, 20.08.2026)
- https://test-omni.x5.ru/files/public/%D0%98%D0%BD%D1%81%D1%82%D1%80%D1%83%D0%BA%D1%86%D0%B8%D1%8F_%D0%9F%D0%BE%D1%80%D1%82%D0%B0%D0%BB_%D0%BF%D0%B0%D1%80%D1%82%D0%BD%D1%91%D1%80%D0%B0_5Post.pdf

The SDK adapter therefore claims only the six capabilities that can be
normalized without guessing a tenant's warehouse, payment or PII contract.
The admitted application operations are the bounded pickup-point directory
read, C2C tariff lookup between two explicit point UUIDs, single-order status
lookup, cancellation by provider order UUID, one PDF label read and a
one-parcel universal order create. Returns, webhooks and courier pickup are
explicit follow-up qualification items, not hidden promises in the manifest.

The connector is admitted to the separate logistics surface for tenant account
configuration, an authenticated credential probe, a bounded pickup-point
directory read, a C2C tariff lookup, a single-order status lookup, cancellation
by provider order UUID, one PDF label read and a one-parcel create with explicit
sender and product-value configuration. Other commercial tariffs remain
fail-closed.
