# Magento (Adobe Commerce open-source) Connector Spec

Family: `storefront`. Self-hosted (host-injected, admin-supplied like WooCommerce/Medusa/Shopware/OpenCart/PrestaShop), Magento REST API v1 against a per-merchant store host, authenticated with a single long-lived Integration access token.

Production networking is host-injected through a typed transport; provider code has no direct Core or SQL authority. Authentication uses a Magento Admin > System > Integrations access token: unlike Shopware's client_credentials OAuth2 exchange, Magento issues one long-lived token at Integration activation time that is used directly as a bearer credential on ordinary REST calls — no runtime token exchange, signing, or refresh is needed on this connector's side at all.

Listing endpoints use Magento's `searchCriteria` bracket-notation query protocol: `searchCriteria[currentPage]`, `searchCriteria[pageSize]`, `searchCriteria[sortOrders][0][field/direction]`, and per-filter `searchCriteria[filter_groups][N][filters][0][field/value/condition_type]`. Magento's documented pagination behavior returns the *last valid page* rather than an empty result when `currentPage * pageSize` exceeds `total_count`; `cursor.go` guards against this proactively by bounding on `total_count` rather than trusting an empty page as an end-of-list signal.

Every Magento SKU is flattened to a single-variant `RemoteProduct` (RemoteID = SellerSKU = SKU); this connector does not model Magento's configurable-parent/simple-child product relationship, matching the precedent already set by WooCommerce and Shopware. Inventory uses the legacy CatalogInventory `stockItems` API against one synthetic location (`magento-store`), not Magento's newer multi-source inventory (MSI) system. Money fields are parsed via `encoding/json.Number` to avoid float rounding. Order timestamps use Magento's non-RFC3339 `"2006-01-02 15:04:05"` UTC format, parsed explicitly.

Official documentation: https://developer.adobe.com/commerce/webapi/rest/ ; entity/field shapes verified against the published Magento core source at github.com/magento/magento2.
