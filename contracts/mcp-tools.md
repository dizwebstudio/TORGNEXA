# MCP Tools Contract

MCP exposes stable provider-neutral TORGNEXA application capabilities, never provider-specific APIs, raw database access, connector credentials or private keys.

## Protocol baseline

Task `018` implements the stateless MCP `2026-07-28` Streamable HTTP baseline at `POST /mcp`:

- mandatory `server/discover`;
- `tools/list`;
- `tools/call`;
- request `_meta` carries `io.modelcontextprotocol/protocolVersion` and `io.modelcontextprotocol/clientCapabilities`;
- protocol headers and body must agree;
- responses are private/no-store application data.

No MCP protocol session stores tenant or authorization state. Tenant scope and trusted agent metadata are reconstructed from authenticated identity on every request.

## Implemented Task-018 tools

### Read
- `commerce.products.search` — permission `commerce.products.read`, risk `read`;
- `commerce.orders.list` — permission `commerce.orders.read`, risk `read`;
- `party.counterparties.search` — permission `party.counterparties.read`, risk `read`.

### Mutation request
- `commerce.price.change.request` — permission `commerce.price.change.request`, risk `write_sensitive`.

`commerce.price.change.request` never writes a price. It can only create a Task-017 approval request for canonical action `pricing.price.updated` / resource type `price`. The caller supplies a canonical UUIDv7/ULID retry id that becomes the durable approval request id; exact replays return the same request, while reuse for a different intent is rejected. The requested intent is SHA-256-bound into approval evidence.

Mutation status vocabulary is:

`completed | queued | approval_required | denied | failed`

Task 018 currently returns `approval_required` or `denied` for the price-change request path; downstream approved execution is deliberately outside the MCP server.

## Reserved capability names

These names remain part of the planned provider-neutral surface but MUST NOT be advertised by `tools/list` until their owning task/port and authorization policy are wired:

Read: `commerce.inventory.get`, `commerce.settlements.list`, `commerce.reports.get`, `commerce.connectors.list`, `commerce.reconciliation.status`, `commerce.claims.list`, `commerce.customer_cases.list`, `social.publications.list`, `social.analytics.get`, `edo.documents.list`, `marking.status.get`, `fulfillment.pickup.list`, `compliance.products.status`, `finance.fx.rates.get`, `security.events.export.status`, `cloud.subscription.get`.

Mutation/request: `commerce.inventory.change.request`, `commerce.reconciliation.run`, `commerce.purchase_order.create.request`, `commerce.claim.create`, `social.publication.create`, `social.publication.schedule`, `edo.document.prepare`, `edo.document.send.request`, `signing.request.create`, `payment.refund.request`, `compliance.document.update.request`, `cloud.subscription.change.request`.

## Safety contract

- organization/workspace always comes from authenticated identity; tool schemas never accept tenant selectors;
- authorization is server-side and is checked both for discovery and execution;
- Task-079 agent governance independently checks exact tool/permission/risk, immutable policy, hard action/frequency limits and tenant/agent/integration kill-switch state;
- missing/unavailable governance state fails closed and tools disappear from discovery when not governed;
- sensitive/legal writes use Task `017` and cannot be executed merely because a model requested them;
- authorized calls append Task-003 audit evidence; inability to record audit evidence fails closed;
- audit summaries record bounded metadata/digests, not raw arbitrary model arguments;
- model text, tool descriptions and external reviews/messages/listings cannot override policy;
- external/model-influenced text is untrusted input and returned source facts are explicitly marked `UNTRUSTED_TOOL_DATA`;
- current counterparty agent output is minimized and does not include INN/registration identifiers;
- bounded provenance records agent/model/run/integration/tool/action/correlation/policy/risk/context class without raw prompt content;
- raw connector secrets, bearer tokens, refresh tokens, signing keys and private keys are never tool results and prohibited capability names cannot be admitted by policy;
- Task `079` AI governance is repository-complete; production identity/control-plane composition remains Task `084`, so `cmd/mcp` stays deny-by-default until that dependency is qualified.
