# Task 071 — KKT/OFD fiscalization abstraction

`internal/platform/fiscalization` defines sale, refund and correction requests without embedding an OFD/KKT vendor. Money uses the existing exact minor-unit primitive. Every request has an immutable external/idempotency reference and may carry marking-code fingerprints/verification status. Corrections must reference the corrected calculation.

Provider status is read back through the provider port and stored as reconciliation evidence. No raw card data belongs to fiscalization. FNS publicly documents fiscal receipt verification and correction-receipt semantics under 54-FZ; vendor transport specifics remain outside the foundation.

Official reference: `https://kkt-online.nalog.ru/` and `https://kkt-online.nalog.ru/ap-description/`.

## Connector SDK boundary

Task 071 adds optional `FiscalReceiptWriter` and `FiscalStatusReader` interfaces to Connector SDK v1. Concrete KKT/OFD adapters implement these interfaces; vendor-specific fields remain outside host business models.
