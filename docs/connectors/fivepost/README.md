# 5Post

5Post is represented by a Connector SDK v1 adapter for the partner delivery
API and is available in the separate «Доставка» surface. The application
runtime supports creating a tenant cabinet, encrypted credential storage, an
authenticated connectivity check, a bounded read of the official pickup-point
directory, a C2C tariff preview, a single-order status lookup, a bounded
cancellation request, a PDF label digest reference and a bounded one-parcel
universal order create; it does not expose product synchronization.

5Post partner access requires an API key issued under a delivery contract. The
partner API exchanges that key for a short-lived JWT; neither credential is
stored or logged by the connector package. A real qualification also requires a
5Post test account and the current partner API contract. The bounded create
route requires explicit sender-warehouse, return-policy, barcode-mode and
product-value configuration. The C2C tariff route requires explicit placement
and issue point UUIDs; other commercial tariffs remain fail-closed.
