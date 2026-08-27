# Yandex Market Connector

Task 033 provides the Yandex Market read baseline for the Partner API. Task 116
adds exact price updates as the connector's only admitted remote mutation.

Baseline surfaces:
- products: `POST /v2/businesses/{businessId}/offer-mappings`;
- store prices: `POST /v2/campaigns/{campaignId}/offer-prices` (or business-wide basic price from offer mappings);
- partner-warehouse stock: `POST /v3/businesses/{businessId}/offers/stocks` plus `POST /v3/businesses/{businessId}/warehouses`;
- grouped/FBY/LaaS stock: `POST /v2/campaigns/{campaignId}/offers/stocks` with host-resolved warehouse IDs;
- orders: `POST /v1/businesses/{businessId}/orders`;
- inbound API notifications: `POST /notification` decoder/ack boundary.
- exact price updates: business-wide or campaign-specific
  `POST .../offer-prices/updates`, followed by asynchronous reconciliation.

Product, inventory and order-status writes remain unadmitted. API keys are
obtained only through Task-021 SecretAccessor and requests use the host-injected
connector transport. Provider code has no direct network, SQL, filesystem,
process, Core, or App authority.

Official references:
- https://yandex.ru/dev/market/partner-api/doc/en/reference/business-assortment/getOfferMappings
- https://yandex.ru/dev/market/partner-api/doc/en/reference/prices/getPrices
- https://yandex.ru/dev/market/partner-api/doc/en/reference/stocks/getStocks
- https://yandex.ru/dev/market/partner-api/doc/en/reference/orders/getBusinessOrders
- https://yandex.ru/dev/market/partner-api/doc/en/push-notifications/reference/sendNotification
