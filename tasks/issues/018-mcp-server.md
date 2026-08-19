# Task 018: MCP server

## Objective
Expose provider-neutral TORGNEXA read capabilities plus one sensitive mutation-request capability through MCP, with the same tenant/authz/approval/audit boundary as the REST application surface.

## Dependencies
003, 017, 026, 081

## Repository implementation
**Completed 2026-08-11.**

Task 018 implements a stateless Streamable HTTP MCP baseline at `POST /mcp`, pinned to protocol `2026-07-28`. The server derives organization/workspace exclusively from authenticated identity, filters tool discovery by authorization, re-authorizes every direct tool call, audits every authorized invocation, and fails closed if audit evidence cannot be recorded.

## Baseline tools
Read:
- `commerce.products.search`
- `commerce.orders.list`
- `party.counterparties.search`

Sensitive mutation request:
- `commerce.price.change.request`

The price tool never updates the price. It resolves the canonical Task-017 policy and creates a `write_sensitive` approval request for `pricing.price.updated`. The caller-generated canonical UUIDv7/ULID idempotency key becomes the durable approval request id. Exact retries return the same request; key reuse for a different intent fails closed. The exact requested price/version/currency/minor-units/reason/idempotency key are bound by SHA-256 to the approval resource identifier.

## Security properties
- no tool accepts `organization_id` or `workspace_id`;
- no raw DB, connector secret, bearer token, private key or provider-specific API is exposed;
- Origin is rejected unless explicitly allowed when present;
- protocol/body/header mismatches fail closed before identity/tool execution;
- `tools/list` is permission-filtered and deterministic;
- `tools/call` performs authorization again even when a client bypasses discovery;
- audit records store a bounded arguments digest, not raw tool arguments;
- an audit failure prevents tool execution;
- production command wiring is deliberately deny-by-default until Task 084 supplies the trusted federated identity adapter; Task 079 must close AI-agent governance before MCP publication.

## Acceptance evidence
- implementation: `internal/app/mcp`, `cmd/mcp`;
- tests cover protocol/discovery, permission filtering, tenant isolation, origin/header rejection, direct-call authorization, audit fail-closed behavior, approval-only price mutation requests, secret-shaped reason rejection and intent binding;
- `contracts/mcp-tools.md`, ADR-0008 and `docs/70-mcp-server.md` describe the implemented baseline and publication boundary;
- architecture review `ARCH-018` records the developer-surface/security/approval impact.

## Follow-ups
Canonical next dependency-ready task is `079 AI agent governance`. Task `084 Enterprise IAM` owns production federation/identity composition; Task `018` intentionally does not invent a second IAM system.
