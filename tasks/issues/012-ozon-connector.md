# Task 012

Create current official Ozon Connector Spec; second reference connector validates provider-neutral SDK.

## Status

Completed.

## Acceptance

Implementation + tests + updated contracts/docs; run required checks.

## Repository completion — 2026-08-10

Repository implementation is complete.

- `ozon` is the second architecture-registered marketplace provider and reuses the existing SDK-v1 `ProductReader` and `InventoryReader` interfaces without a new SDK/Core branch.
- The manifest is read-only: `products.read` and `inventory.read` only. Seller `Client-Id` + `Api-Key` remain one scoped secret reference and plaintext is visible only inside `SecretAccessor.UseSecret`.
- The current official baseline is documented for `/v3/product/list`, `/v3/product/info/list`, `/v2/warehouse/list` and `/v2/product/info/stocks-by-warehouse/fbs` on `api-seller.ozon.ru`.
- Product pagination retains Ozon `last_id` in an opaque connector cursor. Product ID, `offer_id`, warehouse ID and SKU remain remote identities for Task-010 mapping rather than Core fields.
- Inventory reads select bounded seller `offer_id` values and map available quantity as `present - reserved`; partial pagination, duplicate/unrequested IDs, unsafe stock values and malformed/oversized responses fail closed.
- Raw Ozon response bodies, credentials and transport errors do not escape normalized SDK errors.
- Provider code has no direct HTTP/socket/DNS, database, filesystem/process, Core or App authority; network execution remains host-injected.
- Deterministic fixtures cover product list/details, cursor replay, warehouse discovery, FBS stock and adversarial drift/error cases.
- `docs/connectors/ozon/conformance-report.json` passes all 13 mandatory Task-064 checks, including Linux namespace/chroot isolation.

Operational note: repository qualification is deterministic/offline. Production enablement still requires a least-privilege live Ozon seller smoke test and the existing Task-080 hosted architecture controls.
