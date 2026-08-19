# Task 126: MCP client accounts and identity resolver

## Status
`repository-complete` — 2026-08-20.

## Objective
Give `cmd/mcp` a real, tenant-scoped identity path: since Task 018 shipped,
`Run()` has hardcoded `IdentityResolver: denyIdentityResolver{}`, so every
`POST /mcp` request has been rejected. Add a tenant-scoped MCP client
account capability (settings CRUD, one-time bearer token issuance) and wire
it as the production `IdentityResolver`, without inventing a second IAM
system or touching the Connector SDK boundary.

## Dependencies
018

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update
capability/event/API contracts when applicable.

## Acceptance
Account CRUD is tenant-scoped and RLS-forced; credential bytes (the raw
bearer token) never reach Postgres, only a SHA-256 hash; `internal/app/mcp`
gains its first non-deny `IdentityResolver`, verified against a real
running Keycloak-authenticated REST session and the real MCP endpoint, not
just unit tests.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- migration `000014_mcp_client_accounts.sql` adds `mcp_client_accounts`
  (RLS forced, `DELETE`/`TRUNCATE` revoked) storing label/agent_id/model_id/
  integration_id/permissions (`jsonb`, containment-checked against the four
  known MCP tool-permission strings)/`token_hash` (`bytea`, SHA-256)/enabled/version;
- three additive OpenAPI 0.17.0 operations under `/settings/mcp-accounts(:disable)`,
  gated by `settings.mcp_accounts.read` (admin/manager/operator/viewer) and
  `settings.mcp_accounts.write` (admin);
- `internal/platform/mcpaccounts` is a non-branching validation port
  (mirrors `internal/platform/aiadvisory`) that also owns the bearer-token
  encoding (`mcp_<org>.<workspace>.<account>.<secret>`) and hashing;
  `internal/platform/postgres/mcpaccountsrepo` is its repository;
- `internal/app/mcp/identity.go`'s `PostgresIdentityResolver` parses the
  token's embedded routing IDs to build a `tenancy.Scope` before running a
  normal RLS-scoped lookup (Postgres RLS has no scope to enforce before one
  is known; there is no JWT here to carry it the way REST's
  `claimTenantResolver` does), then authenticates with a constant-time
  comparison against the stored `token_hash` — the embedded IDs are only a
  routing hint, never trusted alone;
- `cmd/mcp`'s `Run()` now wires `PostgresIdentityResolver` in place of the
  removed `denyIdentityResolver{}`;
- frontend: `MCPAccountSettings.tsx`, a new dedicated "MCP-агенты" settings
  tab — create/list/disable accounts, per-account tool-permission
  checkboxes, one-time token-reveal dialog;
- deterministic fixtures/tests (`internal/platform/mcpaccounts`,
  `internal/app/mcp/identity_test.go` covering accepted/missing/tampered/
  disabled/wrong-tenant tokens), `ARCH-126`, ADR 0098, capability docs are
  present.

## Verified live (not just unit tests)
Against the running Community Docker stack, with a real Keycloak user
provisioned and authenticated through the full PKCE authorization-code
flow (not a stub): created an MCP client account via
`POST /settings/mcp-accounts` (201), listed it (200), called `POST /mcp`
`tools/list` with the issued token and got **HTTP 200 with a valid
identity-resolved response** — the first request this repository's MCP
endpoint has ever accepted. A tampered token was rejected (401). Disabling
the account via `POST /settings/mcp-accounts:disable` and immediately
retrying the same previously-valid token also returned 401.

The returned tool list is empty and `tools/call` remains denied: Task 079's
real `AgentGovernor` (`internal/platform/agentgovernance`, backed by the
already-implemented and tested `internal/platform/postgres/agentgovernancerepo`)
was discovered during this task to be similarly never composed into
`cmd/mcp` — it is still `unavailableGovernor{}`. That gap, and whether
account creation should auto-install a matching `agentgovernance.Policy`,
is explicitly out of scope here; see ADR 0098's Alternatives section and
`docs/70-mcp-server.md`.

## Qualification
`go build/vet/test -race` pass repo-wide; `gofmt` clean;
`tools/architecturecheck` passes (121 modules / 36 providers / 116
reviews, 0 drift); `make sdk-check` passes (115 operations, OpenAPI
0.17.0); frontend `tsc` (both configs) and the 23-test deterministic
frontend suite pass; `scripts/check-community-deployment.sh` passes
(`deploy/postgres/catalog.tsv` kept in sync with `migrations/catalog.json`).
Migration applied cleanly against the running Docker Postgres (14/14).
