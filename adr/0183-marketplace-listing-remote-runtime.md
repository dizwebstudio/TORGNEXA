# ADR-0183: Marketplace listing remote runtime

Status: Accepted

## Context

The listing workspace already produces a provider-neutral taxonomy and a
deterministic preview. It must not pretend that saving that preview performed a
marketplace write. The platform also already has an immutable publication
snapshot repository and a worker that owns connector calls and status polling.

## Decision

The listing apply API accepts an optional, explicit `publications` plan. Every
row must identify its SKU, immutable `marketplacepublication.Snapshot`, typed
operation and current publication-quality receipt. The API verifies that the
snapshot matches the approved preview and current tenant/account, then stores
the snapshot and enqueues one existing publication operation per row.

The listing repository stores only batch evidence and operation IDs. Snapshot
content and per-row remote receipts remain in the existing publication
repository. This keeps PIM canonical and avoids a second publication ledger.
The publication worker performs connector writes only after the provider
profile, account capability, approval, quality and SecretProvider gates pass.
Retries happen only through its existing state machine, and
accepted/processing/unknown outcomes are resolved with the typed status reader
by remote product ID or asynchronous operation ID. The batch repository write
stores both identities before a worker can claim the operation.

Provider taxonomy is represented by a typed adapter profile. It describes
category/attribute key semantics and the status of live qualification; it is not
a substitute for official provider schema evidence. Raw provider responses,
URLs, access tokens and secrets never enter the core model or batch evidence.

## Consequences

An approved batch can enqueue up to 1,000 independent, idempotent remote
operations and the UI can link the batch to each operation. Partial application
is explicit: only the submitted eligible rows are enqueued. A blank
`publications` list remains a safe evidence-only apply for existing clients.

WB, Ozon and Yandex Market remain `qualification_required` until credentialed
taxonomy, mapping and read-after-write evidence is attached. Synthetic tests
close repository behavior but cannot promote a connector to production-ready.

## Security and compatibility

The change is additive. Batch approval, quality receipts, organization/workspace
scope, immutable snapshots and existing connector capability/SecretProvider
checks remain mandatory. Existing clients may continue sending only `preview`;
new clients use the typed `publications` array. The OpenAPI contract and UI
document both modes.
