# Data Lineage

Task 030 implements immutable, tenant-scoped provenance for important TORGNEXA changes. Lineage complements Task 003 Audit and Task 008 Outbox: audit answers **who/why**, the domain row answers **current state**, the outbox event answers **what integration fact was committed**, and lineage answers **which inputs/transformation produced this output version**.

## Canonical record

A lineage record contains only bounded metadata, never a copy of the business payload:

- source and actor identity;
- operation;
- output system/entity/id/version/field and observed time;
- zero or more ordered input references with role, source system/entity/id/version/field/time;
- transformation kind/id/version plus optional mapping/rule id;
- correlation and causation ids;
- the exact Task-003 `audit_id` and Task-008 `event_id` committed by the same mutation;
- normalized result and UTC occurrence time.

`lineage_records` and `lineage_inputs` are append-only and protected by forced RLS. PostgreSQL rejects a lineage record unless the referenced audit and outbox rows belong to the same organization/workspace.

## Price and stock integration

Task 030 instruments the existing Price and Inventory repositories without changing their public Core contracts.

- Price create records the Offer as an input and the new Price version/`amount` as output.
- Price update additionally records the previous Price version as an input.
- Inventory position create records Offer + Warehouse inputs and the new `inventory_position`/`stock` output.
- Stock mutations additionally record the previous position version.

All of the following commit or roll back together:

`domain state -> audit -> outbox event -> lineage evidence`.

Future PIM, order-status, marking, EDO, publication, FX and settlement modules can append the same generic lineage contract with mapping/rule identifiers instead of inventing provider-specific provenance fields.

## Timeline read API

`GET /api/v1/lineage/timeline` returns immutable records ordered by `(occurred_at,id)` for one system/entity/id and optional field. Pagination uses an opaque pair of `before_at` + `before_id` and is bounded to 200 records.

The reusable HTTP handler requires an injected `LineageScopeResolver`. Organization/workspace identifiers are deliberately **not** accepted from user-controlled query/header values; production composition must resolve the authenticated tenant scope first. The PostgreSQL reader then applies the same scope transaction-locally and relies on forced RLS.

Example query:

`/api/v1/lineage/timeline?system=torgnexa&entity_type=price&entity_id=<id>&field=amount&limit=50`

A record links directly to its audit and event evidence, so UI/API callers can build a change timeline without reconstructing provenance from logs.
