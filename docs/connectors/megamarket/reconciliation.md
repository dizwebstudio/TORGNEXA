# Megamarket Reconciliation

Task 010 mappings keep `goodsId`, `offerId`, `shipmentId`, and warehouse IDs outside Core. Task 013 propagates canonical changes according to policy; Task 014 compares product/inventory/order projections against canonical state and records drift.

Task 034 is read-only, therefore remediation cannot push Megamarket changes. Recommended initial policies are `remote` source-of-truth for imported marketplace order state and explicit/manual policy for catalog/inventory until write capabilities are separately admitted.
