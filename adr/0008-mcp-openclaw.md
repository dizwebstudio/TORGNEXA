# ADR-0008: MCP / OpenClaw boundary

Status: Accepted

## Context

TORGNEXA needs an agent-facing developer surface without creating a privileged automation path around canonical tenancy, authorization, approval, audit, secrets and provider boundaries. The original architectural intent was that OpenClaw and other agents use high-level capabilities rather than raw database/provider access.

Task `018` is the first implementation of this boundary. Task `079` now supplies the immediately-adjacent agent governance layer; Task `017` owns fail-closed approval for sensitive writes, Task `003` owns immutable audit, Task `026` owns canonical search, and Task `084` later owns enterprise identity/control-plane federation.

## Decision

Agents use high-level provider-neutral MCP tools through the normal TORGNEXA application security boundary. MCP is not a privileged automation backdoor and exposes no raw database or secret access.

Task `018` implements a stateless Streamable HTTP MCP `2026-07-28` boundary at `POST /mcp`. Each request reconstructs authenticated identity/tenant scope, applies server authorization, and appends audit evidence. Sensitive tools create/enter the same Task-017 approval workflow used by ordinary application callers instead of directly executing the write. MCP mutation retries use caller-generated canonical request ids and reject id reuse for a different intent.

Tool discovery is permission-filtered, but discovery never grants authority: every `tools/call` is re-authorized. Tenant ids are absent from tool arguments. The baseline command is deliberately deny-by-default until a trusted production identity adapter is supplied.

## Consequences

MCP becomes an additive developer surface over canonical application ports rather than a second business-logic implementation. Tool contracts remain provider-neutral and can expand as their owning modules become available.

Task `079` is repository-complete and closes the AI-governance publication gate. Task `084` still supplies production Enterprise IAM/federation and trusted policy/control-plane adapters rather than MCP inventing competing identity/governance administration.

## Alternatives considered

Giving agents raw SQL/database access was rejected because it bypasses tenant policy, RLS intent, approval and audit.

Giving agents connector/provider credentials or direct provider APIs was rejected because it bypasses canonical models, secrets isolation, conformance and reconciliation.

Trusting `tools/list` as authorization was rejected because a client can directly invoke a known tool name. Execution therefore repeats authorization.

Implementing a separate MCP-specific approval or IAM subsystem was rejected because it would create inconsistent policy and evidence. MCP must reuse Tasks `017` and `084` boundaries.

## Compatibility impact

The change is additive. Existing REST/OpenAPI, events, Connector SDK and provider contracts are unchanged. MCP advertises only tools actually wired and authorized; reserved future tool names are not published prematurely.

The protocol baseline is explicitly versioned as `2026-07-28`; incompatible future protocol behavior requires compatibility review rather than silent semantic drift.

## Migration and data impact

Task `018` itself introduced no persistence. Task `079` adds migration `000026_ai_agent_governance.sql` for immutable agent policy/kill-switch evidence and durable replica-safe frequency receipts/counters; MCP business data still flows only through canonical application ports. Sensitive price changes create existing Task-017 approval request/audit/outbox evidence and do not create a separate MCP business persistence model.

## Security and privacy impact

Organization/workspace and Task-079 agent metadata come only from trusted identity. Origin and protocol metadata/header consistency are validated before execution. Exact permission and agent governance are rechecked at call time, including policy, limits and kill switches. Authorized/governed calls require Task-003 audit evidence; audit failure suppresses the result. Raw model arguments are represented in audit by a SHA-256 digest rather than copied wholesale.

`commerce.price.change.request` is `write_sensitive`, never invokes price update directly, and fails closed without a matching Task-017 policy. Secret-shaped reasons are rejected. Raw connector secrets, bearer/refresh tokens, signing keys and private keys are not tool results.

## Operational impact

`cmd/mcp` shares bounded `TORGNEXA_HTTP_*` listener controls and requires explicit production address configuration. The repository runtime starts with a deny resolver until trusted IAM composition is wired, preventing accidental anonymous exposure.

The MCP and AI-governance repository gates are complete after Tasks `018` and `079`. Production exposure still depends on Task `084`; the current command remains deny-by-default until trusted federated identity/policy composition is supplied.
