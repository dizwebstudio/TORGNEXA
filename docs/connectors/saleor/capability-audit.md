# Capability audit

Only capabilities demonstrated by the current official interface and Connector SDK v1 are admitted.

`products.write` create (`RemoteID == ""`) is declared in the manifest for interface completeness but returns a normalized `unsupported` remote error: Saleor requires a `productType` assignment to create a valid product (`ProductCreateInput.productType: ID!`, which defines the product's entire attribute schema), which `sdk.ProductWriteRequest` does not carry -- defaulting it to some assumed product type would put a real, publicly visible product live with the wrong attribute set.

`products.write` update is supported, but with a documented, deliberate scoping choice: Saleor has no single combined mutation for SKU + display name + publication state the way Magento/Shopware address a whole product row with one PUT. `SellerSKU` lives on `ProductVariant` (`productVariantUpdate`); `Title` and `StatusRemoteID` (publication) live on the shared parent `Product` (`productUpdate`, `productChannelListingUpdate`), since Saleor has no per-variant display name or publication flag to scope them to instead. This connector issues only the mutations needed for the fields that actually changed, then re-fetches once to confirm the full desired state landed. A consequence worth stating plainly: writing `Title` or `StatusRemoteID` through one variant's row affects every sibling variant under the same parent product, because Saleor genuinely has no narrower field to target.

`notifications.receive` is declared but returns `unsupported` -- investigated, not assumed. As of Saleor 3.5, a webhook created without the (deprecated) `secretKey` is signed with a real, verifiable mechanism: a detached JWS using RS256, verifiable against the public key Saleor itself publishes at `/.well-known/jwks.json` on the store's own host, requiring no separate shared secret at all (verified against Saleor's own core source: `saleor/webhook/transport/__init__.py`'s `signature_for_payload` and `saleor/core/jwt_manager.py`'s `JWTManager.jws_encode`). That mechanism does not fit through this Connector SDK's generic webhook envelope: `sdk.CommerceWebhookRequest.Validate` caps `Signature` at 256 bytes, and an RS256 detached JWS's base64url-encoded signature segment alone already exceeds that (a 2048-bit RSA-PKCS1v15 signature is 256 raw bytes, ~342 base64url characters, before even accounting for the JWS header segment). There is consequently no way to carry Saleor's real signature through this envelope without silent truncation, so this fails closed.

`orders.status.write` supports only canceling an order via Saleor's own `orderCancel` mutation; every other Saleor order status (`UNFULFILLED`, `PARTIALLY_FULFILLED`, `FULFILLED`, ...) is a side effect of fulfillment/invoicing workflows with their own mutations, not a directly settable field.

`returns.read` reads `Order.grantedRefunds`, which -- unlike Medusa's and Magento's own return/creditmemo shapes -- carries a real `reason` field, so `sdk.RemoteReturn.Reason` is honestly populated for Saleor rather than left blank.

No browser-cookie automation, private admin-panel endpoints, raw card credentials, or provider-specific Core branches are permitted.

Official documentation: https://docs.saleor.io/api-reference/
