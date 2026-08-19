# 1C mapping and reconciliation profile

Task 015 consumes existing Tasks `010`, `013`, and `014`; it does not create a second mapping or drift subsystem.

## Identity

- `ERPProduct.remote_id` is the configured 1C catalog reference key (`Ref_Key` in the baseline).
- Canonical Product identity is joined only through Task-010 `EntityMapping`.
- Inventory rows reference remote product/location keys and exact quantities; provider keys remain outside Core.

## Authority

Recommended initial policy is `source_of_truth=remote` for catalog/inventory when 1C is the ERP master. If TORGNEXA is authoritative, keep this Task-015 connector read-only and use reconciliation evidence/approval until a separately reviewed ERP write capability exists.

## Reconciliation

- scheduled-full and on-demand scans use deterministic OData ordering and opaque bounded cursors;
- `DataVersion` is retained as remote revision evidence for catalog conflicts;
- `DeletionMark` is an archive signal, never an automatic destructive local delete;
- duplicate remote IDs/balance rows and malformed quantities fail before drift evidence is accepted;
- crash/retry semantics remain Task-013/014 responsibility and operate on stable remote IDs/revisions.

A portable incremental updated-at cursor is not asserted because standard 1C configurations do not guarantee one common field. A configuration-specific change feed must be added as a separate reviewed surface.
