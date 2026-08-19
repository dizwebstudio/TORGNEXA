# CIAN reconciliation

CIAN pulls the configured XML feed; TORGNEXA therefore does not receive a synchronous publication receipt from a `POST publish` operation. The remote reconciliation identity is the import/order ID exposed by CIAN import status/report evidence.

The account is healthy only when remote import-state evidence names the exact configured feed URL. `ReadClassifiedPublicationStatus` then requires the caller's remote import/order ID to equal the returned report identity and requires the same exact feed URL before projecting status.

Projection:
- no processing timestamp and no problems -> `processing`;
- processing timestamp and no problems -> `succeeded`;
- provider problem/error evidence -> `failed`;
- total/inserted/updated/deleted/skipped/errors/notices are carried as bounded aggregate reconciliation counters.

A changed or foreign feed URL, foreign order ID, malformed timestamp/counters or oversized response is a contract failure rather than a best-effort match. Since Task 039 has no API write, there is no write retry ambiguity inside the connector; host-side feed serving remains independently audited and approval/compliance gated.
