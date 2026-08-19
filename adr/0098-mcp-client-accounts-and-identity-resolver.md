# ADR 0098: MCP client accounts as the first working (non-deny) MCP identity path

Status: Accepted

## Context

Task 018 shipped a real MCP JSON-RPC server (`internal/app/mcp`) with tenant
isolation, permission-filtered discovery, fail-closed audit and four tools,
but its production wiring in `Run()` deliberately hardcoded
`IdentityResolver: denyIdentityResolver{}`: every request has been rejected
since the day it shipped. The doc comment attributed this to Task 084
(federated IAM). Task 084 (`internal/platform/enterpriseiam`) turned out to
implement only in-memory SSO claim-to-role mapping for *human* identities —
it has no concept of a machine credential an AI agent presents on an HTTP
call, and nothing anywhere in the repository provisions one. MCP therefore
had no path to ever become reachable without a new, purpose-built capability.

## Decision

Add `mcp_client_accounts` (migration `000014_mcp_client_accounts.sql`, RLS
forced, `DELETE`/`TRUNCATE` revoked, matching `ai_provider_accounts`'s
guard-trigger style): a tenant-scoped account holding label/agent_id/
model_id/integration_id/permissions/enabled/version plus a `token_hash`.
Add three additive OpenAPI 0.17.0 operations under
`/settings/mcp-accounts(:disable)` (create/list/disable), gated by
`settings.mcp_accounts.read`/`write` (read extends to manager/operator/
viewer, write is admin-only, matching `settings.ai_providers.*`).
`internal/platform/mcpaccounts` is the non-branching validation port
(mirrors `aiadvisory`) and also owns token encoding/hashing;
`internal/platform/postgres/mcpaccountsrepo` is its repository.
`internal/app/mcp/identity.go`'s `PostgresIdentityResolver` replaces
`denyIdentityResolver{}` in `cmd/mcp`'s `Run()` — the first non-deny
`IdentityResolver` this repository has ever had composed.

Two departures from `ai_provider_accounts` are deliberate. First, the
credential is inbound (an agent presents it *to* TORGNEXA), reversing
every `secrets.SecretProvider` use here (fetch-by-reference, not
search-by-value), so only a SHA-256 `token_hash` of a 32-byte secret is
stored; the raw token is shown once, at creation, never again. Second,
REST's `claimTenantResolver` reads org/workspace from a verified JWT
before any query runs; MCP has no JWT, and RLS needs a scope first. So
`EncodeToken` embeds org/workspace/account IDs in the token
(`mcp_<org>.<workspace>.<account>.<secret>`); the resolver parses them
into a `tenancy.Scope` *before* a normal RLS-scoped lookup — never a
bypass. The IDs are only routing: authentication is
`subtle.ConstantTimeCompare` of `HashSecret` against `token_hash`, plus
`enabled`.

`Governor`/`Auditor` in `internal/app/mcp/server.go` remain
`unavailableGovernor{}`/`unavailableAuditor{}`, unchanged here — see
Consequences for the gap this leaves open.

## Consequences

`mcp_client_accounts` becomes the first (and, until Task 079's governor is
also wired, the only functionally load-bearing) admission control for
`POST /mcp`. A disabled account's token is rejected immediately on the next
call (verified live: disabling an account via the REST API and immediately
retrying `tools/list` with its previously-valid token now returns 401).

This surfaced a known, separately trackable gap: Task 079's real
`agentgovernance.Service`, backed by the already-implemented and tested
`internal/platform/postgres/agentgovernancerepo`, was never composed into
`cmd/mcp` either. Verified live: with only this change, `tools/list`
returns an empty array (the governor denies discovery for every tool) and
`tools/call` is denied even for a valid, enabled account. Closing that
gap, and whether account creation should auto-install a matching
`agentgovernance.Policy`, is follow-up work, not part of this change.

Token material never appears in Postgres, logs, or the audit trail; the
frontend's one-time reveal dialog (`MCPAccountSettings.tsx`) is the only
place a caller can ever see the raw token.

## Alternatives considered

Routing MCP identity through `internal/platform/enterpriseiam`'s
`ServiceAccount`/`Store` types was rejected: that package has no Postgres
persistence, API, or admin composition anywhere in the repository (its
`Store` is an in-memory `sync.Mutex`-guarded map used only by its own
tests), and its `Evaluate`/`MappingRule` model answers a different
question (map an external SSO claim to a role) than "verify a
self-contained bearer credential and resolve its tenant." Building that
persistence layer out was judged to be Task 084's own scope, not something
to fold into this change.

Requiring the MCP client to send `organization_id`/`workspace_id` as
request headers (instead of embedding them in the token) was rejected: MCP
tool input is explicitly untrusted per Task 018's boundary
("no tool accepts organization_id or workspace_id"), and headers set by the
caller are no more trustworthy than tool arguments for this purpose. The
token is host-issued and its embedded IDs are never trusted without the
hash comparison, which is the same trust model, without adding a second
caller-supplied identity channel.

Fixed `agent_id`/`model_id`/`integration_id` per account (set once at
creation) instead of accepting them per request header was a deliberate,
explicit choice (confirmed with the account's operator): one account maps
to one agent/integration, which keeps the audit trail unambiguous and
avoids inventing a second, non-standard MCP header contract every future
MCP client would need to implement. `run_id` alone is generated fresh per
request, matching `agentgovernance.Agent`'s expectation that a run
identifies one invocation.

## Compatibility impact

Frozen `sdk.Connector`/`sdk.Runtime` roots are untouched — this capability
does not touch the connector plugin boundary at all. `mcp.Identity`,
`mcp.IdentityResolver` and every other exported MCP type are unchanged; only
`Run()`'s composition changed. No existing OpenAPI operation is modified,
only three additive ones.

## Migration and data impact

Migration `000014_mcp_client_accounts.sql` is a normal post-baseline expand
migration adding one new table; the immutable `000001`-`000011` pre-v1
squash and `000012`-`000013` are unchanged.

## Security and privacy impact

`mcp_client_accounts` never stores raw credential bytes. `permissions` is
validated (both in `mcpaccounts.ValidateCreate` and by a `jsonb` containment
`CHECK`) against the fixed four known tool-permission strings only — an
account cannot be granted a permission string that does not correspond to
an actual MCP tool. Create/disable are audited as `write_sensitive`, and the
audit summary excludes the token and the account's permission list is
capped at four entries; no prompt/response text ever flows through this
capability (that remains MCP tool call territory, unaffected by this ADR).

## Operational impact

Disabling an account immediately stops it from authenticating on the next
call — verified live, not just asserted. Because `tools/list`/`tools/call`
remain governor-denied until Task 079's `AgentGovernor` is also wired, an
operator creating an MCP client account today gets a real, auditable
credential but no usable tool access yet; this ADR does not claim
otherwise.
