# Universal Domain Model

Tenancy: `Organization -> Workspace -> Store/BusinessUnit -> ConnectorAccount`.

Core commerce: Product, Variant, Offer, ChannelListing, Price, InventoryPosition, Order, OrderItem, Shipment, Return, Payment.

Vertical: Vehicle, Property, Service, Job. Do not flatten every vertical attribute into Product columns.

Social: Content, ContentVariant, Publication, Campaign, MediaAsset.

Compliance: GTIN, MarkingCode, ComplianceDocument, EDODocument, SigningRequest, CertificateReference, MChDReference, FiscalReceipt, VeterinaryDocument.

Fulfillment: Warehouse, PickupPoint, Carrier, RoutePlan, TrackingEvent.

Money is integer minor units + currency. Never float. Fractional quantities use fixed/decimal representation.

## Canonical catalog (Task 004)

`Product` owns provider-neutral descriptive master data (`code`, `title`, `description`, lifecycle/version). `Offer` is a sellable variation with immutable `sku` and optional validated GTIN. Prices and inventory are deliberately separate Task-005 state.

Lifecycle is forward-only: `draft -> active -> archived`; archived rows remain historical records. An Offer may be created while its Product is draft but cannot become active until the Product is active. A Product cannot archive while a non-archived Offer exists.

Remote provider identities never appear on Product/Offer. They are resolved through the generic connector entity-mapping boundary keyed by tenant + connector account + local entity.

## Canonical price and inventory (Task 005)

`Price` is provider-neutral commercial state attached to a canonical `Offer`. Money is stored and transported only as integer minor units plus a three-letter currency. A Price has an immutable `(offer, kind, currency)` identity and an optimistic version; provider price identifiers and channel-specific pricing rules stay in connector projections. `regular`, `compare_at`, and `cost` are semantic price kinds, while promotions/floor/margin policy remains Task 051.

`Warehouse` is minimal canonical fulfillment identity. `InventoryPosition` is current exact state for `(Offer, Warehouse)` with explicit unit, `on_hand`, `reserved`, and derived `available = on_hand - reserved`. Quantity mirrors the canonical Task-076 Decimal/Quantity representation rather than binary float; PostgreSQL/EventBus adapters convert it to the shared Task-076 wire primitive without loss at storage and wire boundaries. The invariant is `0 <= reserved <= on_hand`; reservations and releases use optimistic versions so concurrent writers cannot silently lose stock updates. Append-only warehouse movement history belongs to Task 054 rather than this current-state aggregate.

Price and inventory mutations commit their domain row, Task-003 audit record, and Task-008 Transactional-Outbox event intent in one PostgreSQL transaction. This gives durable lineage without pretending that Kafka delivery itself is exactly-once.

## Task 006 — canonical Orders

`Order` is the immutable commercial snapshot for a placed order; `OrderItem` snapshots canonical Offer identity, SKU, exact quantity, unit price, line totals and explicit tax treatment. Provider status vocabularies are translated in connector adapters into `pending | confirmed | processing | fulfilled | cancelled`. Remote order identifiers never enter Core and use `connector_entity_mappings(entity_type=order)`. Payments, returns/claims and shipment execution remain separate domains.
