# Capability audit

Only capabilities demonstrated by the current official interface and Connector SDK v1 are admitted.

`products.write` create (`RemoteID == ""`) is declared in the manifest for interface completeness but returns a normalized `unsupported` remote error: Saleor requires a `productType` assignment to create a valid product (`ProductCreateInput.productType: ID!`, which defines the product's entire attribute schema), which `sdk.ProductWriteRequest` does not carry -- defaulting it to some assumed product type would put a real, publicly visible product live with the wrong attribute set.

`products.write` update is supported, but with a documented, deliberate scoping choice: Saleor has no single combined mutation for SKU + display name + publication state the way Magento/Shopware address a whole product row with one PUT. `SellerSKU` lives on `ProductVariant` (`productVariantUpdate`); `Title` and `StatusRemoteID` (publication) live on the shared parent `Product` (`productUpdate`, `productChannelListingUpdate`), since Saleor has no per-variant display name or publication flag to scope them to instead. This connector issues only the mutations needed for the fields that actually changed, then re-fetches once to confirm the full desired state landed. A consequence worth stating plainly: writing `Title` or `StatusRemoteID` through one variant's row affects every sibling variant under the same parent product, because Saleor genuinely has no narrower field to target.

`notifications.receive` is admitted for Saleor's current no-secret webhook mode. Saleor signs that delivery as a detached JWS using RS256 and publishes the verification key at `/.well-known/jwks.json` on the store host. The receiver validates the protected header (`alg=RS256`, `b64=false`, critical `b64`), fetches the matching `kid` through the host-mediated transport, verifies the signature over the exact raw request body, validates the event/resource envelope, and claims a deterministic delivery id for replay protection. The generic SDK envelope accepts a bounded 4096-character JWS so a 2048-bit signature is not truncated. The deprecated `secretKey` HMAC variant remains fail-closed because the current account secret scope contains only the Saleor App bearer token; it must not be inferred or reused as a webhook secret. See the [Saleor webhook overview](https://docs.saleor.io/developer/extending/webhooks/overview) and [payload signature documentation](https://docs.saleor.io/developer/extending/webhooks/payload-signature).

`orders.status.write` supports only canceling an order via Saleor's own `orderCancel` mutation; every other Saleor order status (`UNFULFILLED`, `PARTIALLY_FULFILLED`, `FULFILLED`, ...) is a side effect of fulfillment/invoicing workflows with their own mutations, not a directly settable field.

`returns.read` reads `Order.grantedRefunds`, which -- unlike Medusa's and Magento's own return/creditmemo shapes -- carries a real `reason` field, so `sdk.RemoteReturn.Reason` is honestly populated for Saleor rather than left blank.

No browser-cookie automation, private admin-panel endpoints, raw card credentials, or provider-specific Core branches are permitted.

Official documentation: https://docs.saleor.io/api-reference/
