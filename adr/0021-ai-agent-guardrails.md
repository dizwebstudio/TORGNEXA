# ADR 0021: AI agent guardrails

Status: Accepted

## Context

TORGNEXA exposes provider-neutral automation surfaces to MCP/OpenClaw and future agent runtimes. Model output can be influenced by reviews, messages, catalog text, remote API responses and other content that is not an authority source. A normal API permission check is therefore insufficient: agents also need server-owned risk policy, hard action limits, provenance and an emergency stop that cannot be overridden by prompt content.

## Decision

AI agents are scoped API/MCP clients, never privileged infrastructure. Task `079` adds a fail-closed governance layer between authenticated authorization and tool execution.

- organization/workspace and agent/integration identity come only from trusted server-side identity;
- every tool is classified `read`, `safe_write`, `sensitive_write` or `prohibited`;
- policy is an immutable, tenant-scoped, versioned allowlist for one agent/integration pair;
- safe/sensitive writes require at least one hard action/frequency limit; sensitive writes always require the Task-017 approval boundary;
- prohibited tools, including secret/credential/private-key export capabilities, cannot be enabled by policy or approval;
- external and model-generated text is untrusted data and never grants tenant scope, permissions, policy, limits or approval authority;
- tenant, agent and integration kill switches are append-only versioned state and fail closed when their state cannot be resolved;
- frequency limits are durable and idempotent across replicas; caller retries do not consume a counter twice;
- agent/model/run/integration/tool/action/correlation/policy/risk/context provenance is bounded and attributable without storing raw prompts in audit metadata;
- source/tool facts returned to an agent are marked `UNTRUSTED_TOOL_DATA`; current counterparty MCP output is minimized to display-oriented fields;
- AI-generated recommendations must be distinguishable from source facts in reporting/audit.

Task `079` deliberately does not add secret/private-key MCP tools or direct sensitive execution. Bulk mutation tools are not currently exposed; any future bulk write must provide server-parsed batch limits and dry-run/preview before admission.

## Alternatives considered

- **Prompt keyword filtering** was rejected because prompt injection is semantic and multilingual; authority is instead structurally separated from untrusted text.
- **MCP-only permissions** were rejected because discovery/call permission alone does not bound an agent's monetary, quantity, batch or frequency impact.
- **In-memory rate limits/kill switches** were rejected because multiple replicas would allow bypass and restarts would erase security state.
- **Agent access to credentials/private keys** was rejected because secrets must remain behind existing host/security boundaries and cannot become tool results.
- **A separate agent approval engine** was rejected because it would bypass/inconsistently duplicate Task `017`.

## Compatibility impact

The change is additive to REST/OpenAPI/events/Connector SDK. Existing MCP tool names remain stable, but discovery/execution becomes intentionally stricter: an application permission is no longer sufficient without an active Task-079 agent policy. MCP result `_meta` gains bounded provenance fields and source fact text is explicitly marked untrusted.

## Migration and data impact

Migration `000026_ai_agent_governance.sql` adds forced-RLS, tenant-scoped policy, kill-switch, frequency-window and usage-receipt storage. Policy and kill-switch history is append-only. A trusted control plane may append a new policy version or kill-switch version; re-enable is evidence too, not an in-place edit. No backfill is required and existing application readers/writers remain compatible.

## Security and privacy impact

Missing policy, policy-store errors, kill-switch-store errors, mismatched risk/permission, action-limit violations, unavailable configured frequency limiter and prohibited capabilities all deny execution. External/model text cannot supply authority. Raw prompts are not persisted in governance audit metadata, and the current counterparty MCP projection removes registration identifiers not needed for agent search. Secrets/private keys never become agent tools.

## Operational impact

Tenant, agent and integration kill switches provide immediate operational stop. Versioned policy/kill evidence remains available across process restarts and replicas. Application rollback retains the additive security-evidence tables rather than destructively removing them. Task `084` still owns production federated identity/control-plane composition; `cmd/mcp` remains deny-by-default until that trusted wiring is supplied.

## Consequences

Task `079` closes the repository AI-governance publication gate for MCP but does not claim production exposure. Every future agent write tool must declare a risk class and hard limits; bulk writes additionally require dry-run/preview before admission. Governance failures reduce availability by design in favor of bounded, attributable action.
