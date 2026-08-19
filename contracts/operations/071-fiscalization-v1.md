# 071 Fiscalization contract v1
- kinds: sale/refund/correction;
- exact Money, idempotent fiscal request refs, marking fingerprints;
- correction references the original calculation;
- provider reconciliation status and fiscal-document ref are authoritative evidence.

## Connector SDK boundary

Task 071 adds optional `FiscalReceiptWriter` and `FiscalStatusReader` interfaces to Connector SDK v1. Concrete KKT/OFD adapters implement these interfaces; vendor-specific fields remain outside host business models and the frozen root `Connector`/`Runtime` interfaces are unchanged.
