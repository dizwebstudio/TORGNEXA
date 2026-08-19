# MCP Server

Task `018` establishes the repository MCP boundary for TORGNEXA agents and external MCP clients. It is an application adapter over canonical ports; it is not a second business-logic layer.

## Transport and protocol

The process `cmd/mcp` serves `POST /mcp` using the MCP `2026-07-28` stateless Streamable HTTP model. Supported methods are `server/discover`, `tools/list`, and `tools/call`.

Every MCP request must carry protocol/client capability `_meta`. The `MCP-Protocol-Version`, `Mcp-Method`, and, for `tools/call`, `Mcp-Name` headers must agree with the JSON-RPC body. Origin is validated when present. Request bodies and headers are bounded by server configuration.

The command shares the normal `TORGNEXA_HTTP_*` timeout/address controls. Production requires an explicit `TORGNEXA_HTTP_ADDR` just like the API listener.

## Identity and tenancy

`IdentityResolver` is the trusted ingress adapter. Its result contains actor id, canonical `tenancy.Scope`, permissions, and trusted Task-079 agent metadata (`agent_id`, `model_id`, `run_id`, `integration_id`). Tool JSON never accepts organization/workspace or agent-authority selectors.

The repository baseline deliberately wires `cmd/mcp` to a deny resolver/governor until the production identity and policy adapters are composed. This prevents an accidental anonymous or ungoverned MCP deployment. Task `084` owns Enterprise IAM/federation and trusted control-plane composition; Tasks `018`/`079` define the application/governance boundaries it must satisfy.

## Authorization

`tools/list` includes only tools for which both `Authorizer` and Task-079 `AgentGovernor` allow the authenticated identity/agent. A client cannot turn discovery into authorization: `tools/call` checks application authorization and agent governance again for the exact tool/permission/risk.

The baseline `ExactPermissionAuthorizer` recognizes exact application permission strings. Production IAM may map federated roles/groups/scopes into the same permissions, but cannot move tenant/agent selection into model input. Missing policy, kill-switch-store failure, risk/permission mismatch or configured frequency-limiter failure denies the tool.

## Read tools

Three read tools are wired in Task 018:

- product search -> Task `026` `search.Provider`;
- order list -> Task `026` `search.Provider`;
- counterparty search -> Task `081` legal-party search port.

All providers receive the tenant scope from identity. No connector-specific fields are exposed. Returned source text is marked `UNTRUSTED_TOOL_DATA`; counterparty output is minimized to party type/id, code, display name and status for the current agent surface.

## Sensitive price-change request

`commerce.price.change.request` validates canonical `PriceID`, optimistic `expected_version`, ISO-style uppercase currency, non-negative minor units, bounded reason, and a caller-generated canonical UUIDv7/ULID retry id.

The tool does **not** invoke `pricing.Repository.Update`. It resolves Task `017` for:

- action: `pricing.price.updated`;
- resource type: `price`;
- risk: `write_sensitive`.

No active matching policy returns `denied`. A matching policy creates an approval request with source `mcp`. The retry id becomes the durable Task-017 approval request id. An exact replay returns the same existing request even if the active policy later changes; using the same retry id with a different intent fails closed. The executable intent is canonicalized and hashed; `price_id#sha256=<digest>` becomes the approval resource id. This prevents an approval request from ambiguously representing a different requested price payload.

Before approval creation, Task-079 governance checks the server-parsed requested currency/minor-units against the active agent policy. The reason is rejected if Task-017 secret sanitization would redact it. Raw model arguments are not copied into MCP audit summaries.

## Audit

Before executing any authorized `tools/call`, the MCP boundary appends Task-003 authorization evidence with:

- actor and tenant from identity;
- source `mcp`;
- action `mcp.tool.<tool-name>`;
- risk class;
- phase `authorized`;
- SHA-256 of arguments;
- bounded agent/model/run/integration and governance policy/version/context-trust provenance.

Governance authorization occurs before this boundary audit; the audit occurs before any tool side effect. If audit capture fails, MCP does not execute the tool and returns an `audit_failed` tool error. This intentionally favors evidence integrity over availability. Domain mutations then append their own transactional outcome audit/outbox evidence inside canonical services.

The approval repository independently writes its own Task-017 audit/outbox evidence transactionally when a price approval request is created.

## Publication boundary

Tasks `018` and `079` are repository-complete, so the AI-governance publication gate is closed. Production exposure is still not claimed: Task `084` must provide trusted federated identity plus policy/control-plane composition, and the current `cmd/mcp` remains deny-by-default until that wiring is qualified.
