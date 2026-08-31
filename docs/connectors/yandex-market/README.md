# Yandex Market Connector

Task 033 provides the Yandex Market read baseline for the Partner API. Task 116
adds exact price updates, and the current runtime also admits exact inventory
updates through the documented partner-warehouse and grouped-warehouse APIs.

Baseline surfaces:
- products: `POST /v2/businesses/{businessId}/offer-mappings`;
- store prices: `POST /v2/campaigns/{campaignId}/offer-prices` (or business-wide basic price from offer mappings);
- partner-warehouse stock: `POST /v3/businesses/{businessId}/offers/stocks` plus `POST /v3/businesses/{businessId}/warehouses`;
- grouped/FBY/LaaS stock: `POST /v2/campaigns/{campaignId}/offers/stocks` with host-resolved warehouse IDs;
- orders: `POST /v1/businesses/{businessId}/orders`;
- inbound API notifications: `POST /notification` decoder/ack boundary.
- exact price updates: business-wide or campaign-specific
  `POST .../offer-prices/updates`, followed by asynchronous reconciliation.
- exact inventory updates: `POST .../v3/businesses/{businessId}/offers/stocks/update`
  for cabinets without warehouse groups, or `PUT .../v2/campaigns/{campaignId}/offers/stocks`
  for configured warehouse groups; both are accepted asynchronously and
  confirmed by a later inventory reconciliation scan.

Product and order-status writes remain unadmitted. API keys are
obtained only through Task-021 SecretAccessor and requests use the host-injected
connector transport. Provider code has no direct network, SQL, filesystem,
process, Core, or App authority.

Official references:
- https://yandex.ru/dev/market/partner-api/doc/en/reference/business-assortment/getOfferMappings
- https://yandex.ru/dev/market/partner-api/doc/en/reference/prices/getPrices
- https://yandex.ru/dev/market/partner-api/doc/en/reference/stocks/getStocks
- https://yandex.ru/dev/market/partner-api/doc/en/reference/stocks/updateStocks
- https://yandex.ru/dev/market/partner-api/doc/en/reference/stocks/updateStocksOnPartnerWarehouses
- https://yandex.ru/dev/market/partner-api/doc/en/reference/orders/getBusinessOrders
- https://yandex.ru/dev/market/partner-api/doc/en/push-notifications/reference/sendNotification
