# Task 019 — n8n Node

Status: **repository-complete**

Ship a separate repo-ready `n8n-nodes-torgnexa` community-node package. n8n remains external; no embedded/white-labelled runtime is introduced.

## Delivered

- `TorgnexaApi` credential with absolute `/api/v1` base URL and scoped bearer credential.
- Client-side tenant/workspace selectors are forbidden; authenticated identity remains authoritative.
- Shared public REST client with redirect blocking, bounded timeout, status validation and RFC7807-safe error projection.
- `TORGNEXA` node with Product list/search and Order list/search against Task-026 public search endpoints.
- `TORGNEXA Trigger` with current canonical event options plus forward-compatible additional canonical event types.
- Dynamic webhook activation using a fresh signing secret; exact-raw-body HMAC-SHA256 verification and five-minute replay protection.
- Workflow deactivation uses the additive public disable endpoint `DELETE /webhook-subscriptions/{id}`. Disable preserves delivery history and revokes signing material.
- No raw DB, SecretProvider, Connector SDK internal, MCP, private signing-key or provider-specific access.
- Offline TypeScript/unit suite for URL/query isolation, API transport, event validation and webhook signature behavior.

## Security boundary

n8n is an ordinary external principal. Sensitive writes remain approval-aware public API operations; Task 019 adds no privileged bypass. Webhook signing material exists only in n8n workflow static state and TORGNEXA SecretProvider; it is never returned by the TORGNEXA management API.

## Acceptance

Implementation + tests + updated contracts/docs; required repository checks run where supported. Task `019` is complete and the canonical next Phase-2 task is `078 Plugin Marketplace Governance`.
