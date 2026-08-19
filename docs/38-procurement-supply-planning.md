# Procurement & Supply Planning

TORGNEXA must support the inbound half of commerce, not only sales.

## Core entities

Supplier, SupplierOffer, PurchaseOrder, PurchaseOrderLine, InboundShipment, ReceivingPlan, LeadTime, ReorderPolicy, Forecast, ReplenishmentRecommendation.

## Planning inputs

- available/reserved/in-transit stock;
- sales velocity and seasonality;
- confirmed marketplace demand/signals;
- supplier lead time and MOQ;
- safety stock and target days-of-supply;
- warehouse capacity;
- promotions and advertising plans.

## Rules

Recommendations are advisory by default. Creating or sending a purchase order is a separate approved write operation. All recommendations record the input snapshot and algorithm/version that produced them.
