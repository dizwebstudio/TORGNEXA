# 1C Connector Spec v1

Status: repository-qualified read-only ERP reference connector  
Snapshot date: 2026-08-10  
Connector ID: `onec`  
Display name: `1C`  
Connector SDK: v1

`onec` is used because Connector SDK IDs must begin with a lowercase ASCII letter; provider-specific 1C identifiers still never enter Core.

## Official platform baseline

Task 015 uses the standard 1C:Enterprise automatic REST interface (OData), not a configuration-specific proprietary API.

Official references:

- REST interface overview: `https://v8.1c.ru/platforma/rest-interfeys/`
- Integration overview: `https://v8.1c.ru/platforma/integraciya/`
- OData integration documentation: `https://its.1c.ru/db/intgr83/content/48/hdoc`
- OData paging/options (`$top`, `$skip`, `$orderby`): `https://wonderland.v8.1c.ru/blog/rasshirenie-podderzhki-protokola-odata/`
- Automatic REST interface and accumulation-register `Balance()`: `https://wonderland.v8.1c.ru/blog/avtomaticheskiy-rest-interfeys-prikladnykh-resheniy/`
- Web authentication setup: `https://1c-dn.com/anticrisis/tools-and-technologies/embedded-web-client/setting-up/`

The platform documents the standard interface as OData v3 and supports JSON responses. The exact resource and field names depend on the published applied solution, therefore they are account configuration rather than hard-coded provider logic.

## Authentication

The baseline uses 1C/web-server Basic authentication over TLS. The secret payload is two UTF-8 lines: username, then password. It remains behind the Task-021 `SecretAccessor` callback and is never persisted/logged by the provider.

Anonymous publication is intentionally not the Task-015 production baseline.

## Host-owned configuration

Non-secret configuration is supplied by a host-injected `ConfigurationSource`, keyed by the validated connector account. It is deliberately not added to the frozen SDK-v1 `Runtime` root.

Required configuration:

```text
host = erp.example.ru
base_path = /trade/odata/standard.odata

catalog.resource = Catalog_Номенклатура
catalog.id = Ref_Key
catalog.code = Code
catalog.sku = Артикул            # optional
catalog.title = Description
catalog.brand = Бренд            # optional
catalog.revision = DataVersion
catalog.archived = DeletionMark

inventory.resource = AccumulationRegister_ТоварыНаСкладах
inventory.function = Balance
inventory.product = Номенклатура_Key
inventory.location = Склад_Key
inventory.quantity = КоличествоBalance
```

Resource/field names are validated as OData identifiers. `base_path` must end in `/odata/standard.odata`; traversal/query/fragment/control characters are rejected. The provider receives only host/path/query structures and has no socket/DNS/HTTP implementation authority.

## Health

Health reads `GET <base_path>/$metadata`. A 2xx response must contain bounded XML metadata evidence (`Edmx`); an HTML login page or empty/oversized body is not accepted as healthy.

## `erp.catalog.read`

Catalog pages use:

```text
GET <base_path>/<catalog.resource>
  ?$format=json
  &$select=<configured fields>
  &$orderby=<id field> asc
  &$skip=<offset>
  &$top=<limit>
```

Projection:

- configured reference key -> `ERPProduct.remote_id`;
- code -> `code`;
- optional article/SKU -> `sku`;
- description/name -> `title`;
- optional brand -> `brand`;
- `DataVersion` or configured equivalent -> opaque remote `revision`;
- `DeletionMark` or configured equivalent -> `archived`.

The connector cursor is opaque Base64URL metadata containing only version, offset and a SHA-256 fingerprint of the selected publication/mapping. A cursor is rejected after mapping/host changes.

## `erp.inventory.read`

Inventory reads the configured accumulation-register virtual table:

```text
GET <base_path>/<inventory.resource>/Balance()
  ?$format=json
  &$select=<product,location,quantity>
  &$orderby=<location> asc,<product> asc
  &$skip=<offset>
  &$top=<limit>
```

The quantity is preserved as an exact bounded decimal string. Floating-point conversion and exponent notation are forbidden. Negative balances, duplicate `(location, product)` rows, malformed identities and oversized responses fail closed.

## Mappings and reconciliation

Task 015 does not add 1C IDs to Product/Inventory Core structs.

- catalog reference key -> Task-010 `EntityMapping(entity_type=product)` remote ID;
- warehouse/location key -> remote inventory location identity;
- `DataVersion` -> remote revision/conflict evidence;
- `DeletionMark=true` -> archived remote catalog state, handled by Task-013 source-of-truth policy rather than implicit hard delete;
- Task-014 scheduled-full/on-demand reconciliation compares the mapped canonical state with these read projections.

The standard OData baseline does **not** claim a portable configuration-independent updated-at field. Therefore Task 015 does not fabricate incremental time cursors. Deployments that need true change feeds must expose an explicitly reviewed field/service or a later adapter; full reconciliation remains deterministic via ordered `$skip/$top` pages.

## CommerceML

CommerceML is intentionally **not** implemented in Task 015. It is a separate adapter/protocol surface and must receive its own security, idempotency, import and reconciliation review rather than being hidden inside the OData connector.
