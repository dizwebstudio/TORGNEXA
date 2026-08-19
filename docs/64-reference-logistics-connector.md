# Reference Logistics Connector

The generic Carrier/Logistics SDK must be proven by one production reference connector. The initial reference should target a broadly used carrier selected after a current API/capability audit (default candidate: CDEK).

## Required reference flow

- calculate tariff/SLA;
- create/cancel shipment;
- obtain label/waybill where supported;
- track status through polling/webhooks;
- pickup-point directory/selection when exposed;
- return shipment flow;
- idempotency/mapping/retry/error normalization;
- reconciliation of remote vs TORGNEXA shipment status.

Provider-specific services/tariff codes stay in connector mappings/extensions. Fulfillment Core uses normalized shipment/routing capabilities.
