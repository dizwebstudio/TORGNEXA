# CIAN connector

Task 039 implements the `cian` classified/vertical adapter for residential property XML publication and import-status reconciliation. CIAN publication is pull-based: TORGNEXA builds a bounded XML feed that the account exposes at a registered URL; the connector does not pretend that CIAN offers an API `publish` mutation.

Qualified runtime capability:
- `classified.publications.status.read`.

Provider-specific host helper:
- deterministic CIAN Feed v2 XML generation for bounded `flatSale` and `flatRent` objects.

Not admitted in Task 039: API push publication, arbitrary listing reads, new-building/Jk-specific payloads, suburban/commercial categories, promotion writes, chat/leads and analytics. See `capability-audit.md` and `property-mapping.md`.
