# Task 129: Trust control plane and decision labs

## Status
`complete` — 2026-08-24. Product, security, repository-policy and Community
runtime acceptance are verified. Release-topology P3/P4, hosted GitHub/OIDC
evidence and live-provider qualification remain external release gates rather
than Task 129 implementation work.

## Objective
Close the security and product gaps discovered by the repository audit with a
single tenant-scoped trust foundation and three operator-facing capabilities:
governed AI egress, connector replay, and profitability scenarios.

## Dependencies
079, 084-085, 092, 103, 109, 120, 122, 126

## Deliverables

### P0 — runtime and human identity
- [x] fail startup when a production runtime database role is superuser,
  `BYPASSRLS`, schema owner, or able to create roles/databases;
- [x] provision a separate Community application role after migrations and use
  it for API/worker/scheduler/MCP;
- [x] expose a minimized admin-only runtime security posture view;
- [x] resolve every human request through an active `workspace_members`
  binding and use the database member role as the permission authority;
- [x] support reviewed invite-to-OIDC binding and immediate disable/revocation.

### P1 — durable trust services
- [x] add tenant-scoped idempotency receipts binding operation, key and request
  digest to one terminal resource/result;
- [x] add an append-only security evidence ledger for sensitive mutations,
  external egress decisions and replay/scenario execution;
- [x] add MCP credential expiry, rotation, last-used evidence and immediate
  revoke; expose the lifecycle through settings APIs;
- [x] make MCP use the same trusted-proxy/origin/rate-limit edge policy as REST;
- [x] make sensitive account/policy writes fail closed when durable evidence
  cannot be committed.

### P2 — governed operator capabilities
- [x] AI egress policy: allowed data classes, redaction, provider/model allowlist,
  per-request and monthly budgets, preview and durable usage evidence;
- [x] Connector Replay Lab: saved synthetic fixture admission and deterministic
  no-remote-call results, with no production credentials or write authority;
- [x] Profitability Scenario Lab: immutable input snapshots and fixed-decimal
  what-if calculations for price, fees, advertising, logistics, FX and quantity;
- [x] protected OpenAPI operations, generated SDKs and operator UI for all three.

## Safety invariants
- runtime services never connect as a table owner, superuser or `BYPASSRLS`;
- OIDC claims are routing hints; active database membership is authoritative;
- no raw bearer/provider credential is persisted in receipts, evidence or audit;
- replay fixtures are synthetic/bounded and cannot execute remote writes;
- AI prompt/response bodies are not audit/evidence payloads;
- profitability calculations use integer minor units/fixed decimal and record
  their immutable assumptions/version;
- all new records use forced tenant RLS and append/lifecycle guards.

## Acceptance
- success and failure/idempotency/revocation cases have deterministic tests;
- OpenAPI, SDKs, migrations, architecture review and operator docs are current;
- `gofmt`, `go test ./...`, `go vet ./...`, contract, frontend and supply-chain
  gates pass using the patched Go toolchain;
- Community compose starts with the non-privileged application role and the
  runtime posture reports PASS.

## Validation

- full Go 1.26.7 test and vet suites: PASS;
- `govulncheck`: PASS, no reachable vulnerabilities;
- contracts/runtime parity and generated SDK drift/tests: PASS, 129 operations;
- frontend typechecks, 24 tests, static policy and production build: PASS;
- migration/baseline and isolated PostgreSQL 18 role/RLS/append-only smoke: PASS;
- architecture review: PASS, 124 modules / 38 providers / 120 reviews;
- Community static deployment policy, JS supply-chain policy and scratch runtime
  build: PASS;
- aggregate repository supply-chain policy: PASS; the checker now accounts for
  both constrained CI jobs, all six registered Go modules, registered npm and
  Python surfaces, Compose anchors, pinned Node images, release binaries and
  explicitly source-only commands without relaxing unknown-input denial;
- the existing Community `.env` was upgraded without rotating prior secrets,
  the stack was rebuilt/recreated, migration 16 applied, and API/MCP/frontend
  report healthy. Live PostgreSQL evidence shows seven `torgnexa_app` sessions,
  no owner/superuser/BYPASSRLS/CREATE privilege and FORCE RLS on all seven trust
  tables.
