# Migration 000014 — Data Lineage

Expand-only migration adding immutable `lineage_records` and `lineage_inputs`.

Safety properties:

- no existing table/column is renamed or dropped;
- old readers/writers remain compatible;
- forced RLS is enabled on both new tables;
- application roles receive SELECT/INSERT semantics only;
- UPDATE/DELETE/TRUNCATE are blocked by triggers;
- each record validates that referenced Audit and Outbox evidence belongs to the same tenant;
- timeline and source indexes are additive;
- no business payload, credentials or arbitrary metadata blob is introduced.

Binary rollback leaves lineage evidence intact. Do not drop these tables during rollback because they form historical provenance evidence.
