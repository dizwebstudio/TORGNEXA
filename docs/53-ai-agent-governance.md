# AI Agent Governance

Task `079` implements the fail-closed governance boundary for MCP/OpenClaw and future agent runtimes. Agents are ordinary scoped clients; model text is never an authority source.

## Authority model

Trusted server-side identity supplies `organization/workspace` plus `agent_id`, `model_id`, `run_id` and `integration_id`. Tool arguments, reviews, messages, product content, remote provider payloads and model output cannot select or override any of those values.

Authorization is intentionally two-dimensional:

1. normal application permission/RBAC must allow the tool; and
2. an active immutable AI-agent policy must allow the exact tool, permission and risk class.

Missing or unavailable governance state denies discovery and execution.

## Risk classes

- `read` — source-data read; no approval;
- `safe_write` — bounded write; hard action/frequency limits are mandatory and untrusted-context direct execution is denied by default;
- `sensitive_write` — hard limits plus Task-017 approval are mandatory; the agent cannot directly execute the sensitive action;
- `prohibited` — cannot be made executable by policy or approval. Secret, credential, refresh-token and signing/private-key export capabilities belong here and are never MCP tools.

`contracts/ai/tool-risk-policy.yaml` is the human-readable baseline; `contracts/ai/agent-policy-v1.schema.json` is the machine-readable policy contract.

## Hard action limits

Policies can cap exact integer-minor-unit money by currency, quantity, percentage in basis points, batch size and calls per bounded time window. Money never uses floating point. A safe/sensitive write policy with no hard limit is invalid.

The server parses semantic metrics from validated tool input before governance. For `commerce.price.change.request`, the requested canonical currency/minor-units are checked before a Task-017 approval request can be created. The policy does not contain a universal TORGNEXA business-value cap: each tenant/agent policy must explicitly define the permitted limit.

Frequency accounting is durable PostgreSQL state. `invocation_id` receipts are idempotent across retries, and an advisory transaction lock serializes a policy/tool/window across replicas. Reusing one invocation for different policy/tool/window semantics fails closed.

## Kill switch

The kill switch resolves three independent scopes:

- tenant (`*`);
- exact agent;
- exact integration.

Any disabled scope denies discovery/execution. State is append-only/versioned; re-enabling creates another version. Database/store failure is treated as disabled from the caller's perspective.

## Prompt-injection boundary

Task `079` does not attempt to identify prompt injection by matching phrases such as “ignore previous instructions”. Instead, external/model-influenced context is classified as untrusted and is structurally unable to supply tenant identity, agent identity, permissions, policy, limits, kill-switch state or approval authority.

MCP source facts are prefixed `UNTRUSTED_TOOL_DATA`. Counterparty search additionally minimizes output to party type/id, code, display name and status; INN/registration identifiers are not returned by that agent read surface. Raw prompts and arbitrary provider responses are not copied into governance audit metadata.

`contracts/ai/prompt-injection-regressions-v1.json` is an executable adversarial corpus. Tests prove that malicious product/review text cannot create write authority, over-limit price intent is denied, sensitive price intent remains approval-only and private-key export remains prohibited.

## Provenance

Successful MCP calls attach bounded provenance including agent/model/run/integration, tool/action, correlation id, policy id/version, risk and context-trust class. The same safe identifiers are present in Task-003 boundary audit metadata. Source facts use `output_kind=source_facts`; the approval-request workflow uses `output_kind=governance_workflow`; neither is falsely marked AI-generated.

Future model-authored recommendations must explicitly set `ai_generated=true` and use an AI-recommendation output kind so reporting/audit can distinguish them from source facts.

## Bulk operations

Task `079` exposes no bulk mutation tool. Any future bulk write must have a bounded `batch_size` policy, server-side preview/dry-run and the same approval/idempotency/audit rules before it can be admitted. An unbounded write rule is invalid by construction.

## Persistence

Migration `000026_ai_agent_governance.sql` adds forced-RLS tenant-scoped tables:

- `ai_agent_policies` — immutable policy versions with trusted actor/reason evidence;
- `ai_agent_kill_switches` — immutable tenant/agent/integration operational-state history;
- `ai_agent_call_counters` — bounded monotonic frequency windows;
- `ai_agent_call_usage` — append-only idempotency receipts.

Repository methods support trusted control-plane policy installation and kill-switch changes; agent-facing code exposes only resolve/enforcement ports.

## Publication boundary

Task `079` closes the repository AI-governance gate for MCP. Production federation and trustworthy population of agent identity/policy control-plane actors remain Task `084 Enterprise IAM`; the current `cmd/mcp` composition therefore stays deny-by-default until that dependency is wired and qualified.
