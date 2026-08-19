# WooCommerce Connector Spec

Family: `marketplace`. Provider ID: `woocommerce`. SDK: v1.

## Binding

Each connector account binds exactly one HTTPS WooCommerce store host, optional WordPress base path and one store currency. The host is configuration, not a credential. Raw IPs, localhost, `.local`, URL queries/fragments and `..` base paths are rejected. Host transport is responsible for TLS verification and egress/DNS policy.

Credentials are a Task-021 opaque secret containing Consumer Key, Consumer Secret and a dedicated WooCommerce webhook secret. They exist only inside the SecretAccessor callback. Consumer credentials are never placed in query strings.

## Canonical identities

- product: Woo product numeric ID (`RemoteProduct.RemoteID`);
- simple purchasable variant: `product:<product_id>`;
- variation: `variation:<product_id>:<variation_id>`;
- order/refund: Woo numeric IDs.

These IDs remain connector mapping keys and do not enter Core structs.

## Read behavior

Product and order full scans use bounded Woo pagination and opaque account/configuration-bound cursors. Variable products fetch bounded variation pages. Price reads preserve store currency. Inventory exposes one virtual store location and returns quantities only when Woo has an explicit `stock_quantity`; unmanaged stock fails as unsupported.

## Write behavior

Commerce writes use the additive SDK-v1 `ProductWriter`, `PriceWriter`, `InventoryWriter` and `OrderStatusWriter` interfaces; the frozen root `Connector` and `Runtime` contracts are unchanged. Writes must still pass host Task-017 approval/risk and Task-082 product-compliance policy before invocation.

## Webhooks

The receiver requires a host-known expected topic, rejects a mismatching header topic, verifies the base64 HMAC-SHA256 body signature, derives replay identity from signed material and delegates durable claim to the Task-009-style host deduplicator. Header delivery IDs are not trusted as replay identity because Woo signs the body, not those headers.

## Webhook privacy

Signature verification and replay identity use the exact raw body, but the provider emits only a minimized canonical envelope (`id` and provider GMT timestamps). Billing, shipping, customer identity and other webhook body fields are not copied into the Connector SDK result.
