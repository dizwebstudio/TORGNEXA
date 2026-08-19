# ADR 0050: External n8n node boundary

Status: Accepted

## Context

TORGNEXA needs n8n automation without turning n8n into an internal privileged runtime. Embedding n8n, exposing database/internal service access, accepting client tenant selectors, or leaving webhook subscriptions active after workflow deactivation would bypass the public API/security boundary and create lifecycle/security drift.

## Decision

Ship `n8n-nodes-torgnexa` as a separate repo-ready community-node package under `integrations/`. n8n stays an external principal and calls only TORGNEXA public REST/webhook surfaces with a scoped credential. Tenant/workspace scope is derived by TORGNEXA from authenticated identity; the node exposes no organization/workspace selector and rejects such query keys in its shared client.

The Task-019 baseline exposes generic Product and Order list/search operations only. It deliberately exposes no raw-request operation and no internal database, Connector SDK, SecretProvider, MCP, signing-key or provider-specific access. Sensitive mutations remain public approval-aware operations owned by the relevant domain/Task-017 policy and are not added by this task.

The trigger uses Task-063 durable signed subscriptions. Activation creates a fresh subscription/signing secret and stores the receiver copy in n8n workflow static state, following the native n8n trigger lifecycle pattern. Deactivation calls additive `DELETE /webhook-subscriptions/{id}`; this is a lifecycle disable, not hard deletion: delivery evidence remains and TORGNEXA revokes signing material. Incoming requests are verified against the exact raw body plus delivery id/timestamp before parsing/forwarding, with a five-minute replay window and configured event allowlist.

The package targets the current programmatic community-node layout with Node.js 22+, strict node metadata and `n8n-workflow` as the only runtime peer. TORGNEXA does not redistribute or embed n8n.

## Consequences

n8n workflows use the same identity, authorization, approval, audit and webhook boundaries as every other external client. Provider additions do not require node releases unless they add a generic public domain capability. Workflow deactivation cannot leave an intentionally active signing endpoint behind, while immutable webhook delivery evidence is retained.

Task `078 Plugin Marketplace Governance` remains responsible for package admission/publishing governance, provenance and marketplace policy. Task `084 Enterprise IAM` remains responsible for production federated identity/control-plane composition.

## Alternatives considered

Embedding n8n was rejected because it creates a second privileged execution plane. A generic raw HTTP/raw SQL node was rejected because it bypasses typed contracts and approvals. Client-selected tenant/workspace fields were rejected because authenticated identity is authoritative. Hard-deleting webhook subscriptions was rejected because it destroys operational evidence. Verifying a re-serialized JSON body was rejected because signatures must bind the exact bytes received.

## Compatibility impact

OpenAPI is additively bumped to `0.7.0` with `DELETE /webhook-subscriptions/{subscription_id}` and generated SDKs now contain 30 operations. Existing webhook create/list/rotate and domain operations are unchanged.

## Migration and data impact

No database migration is required. Existing webhook subscription status already supports `disabled`; repository logic performs an idempotent active-to-disabled transition and preserves rows/history.

## Security and privacy impact

Credentials remain in n8n credential storage; the dynamically generated webhook signing secret is held in n8n workflow static state and TORGNEXA SecretProvider, and is not returned by management reads. Base URLs require HTTPS except loopback development, redirects are disabled, tenant selector injection is rejected, raw webhook bytes are mandatory, and error projection excludes raw server payloads.

## Operational impact

Operators must expose an HTTPS n8n webhook URL and protect n8n workflow state as sensitive application data. Disabling/reconfiguring a workflow disables the old TORGNEXA subscription and revokes its secret. Task-063 retention/DLQ/replay evidence remains authoritative.
