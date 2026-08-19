# Task 079: AI agent governance

## Objective
Implement MCP/tool risk policy, agent-scoped action limits, provenance, kill switch and prompt-injection regression fixtures.

## Dependencies
018, 017, 030, 060

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Agents cannot access secrets/private keys or bypass approval; external text treated untrusted.

## Repository implementation

**Completed 2026-08-11.**

- Added `internal/platform/agentgovernance` with server-owned immutable tool policy, four risk classes, exact money/quantity/percentage/batch/frequency limits and fail-closed policy/kill-switch/frequency ports.
- Added tenant/agent/integration versioned kill switches and durable, idempotent replica-safe frequency enforcement in `internal/platform/postgres/agentgovernancerepo`.
- Added migration `000026_ai_agent_governance.sql` with forced RLS, append-only policy/kill/usage evidence and monotonic call counters.
- Wired governance directly into MCP discovery and execution after normal permission checks. Missing policy or governance-store failures deny access.
- Sensitive `commerce.price.change.request` remains approval-only through Task `017`; governance checks exact requested money before approval creation.
- Secret/private-key capability names are prohibited and cannot become executable through approval or a malformed policy.
- Added bounded agent/model/run/integration/tool/action/correlation/policy provenance to MCP results and Task-003 audit metadata without raw prompt arguments.
- Treat all MCP external/model-influenced context as untrusted authority; source facts are marked `UNTRUSTED_TOOL_DATA` and counterparty output is minimized.
- Added machine-readable policy/provenance contracts and executable prompt-injection regression corpus.
- Expanded ADR-0021 and MCP/AI-governance documentation, and registered ARCH-079.

## Acceptance evidence

- External product/review text cannot grant a write or secret capability.
- Direct sensitive execution remains impossible: the only current sensitive MCP mutation creates Task-017 approval evidence.
- Missing/failed policy or kill-switch state is fail-closed; a configured frequency limit also fails closed without its limiter.
- Tenant, agent and integration kill switches independently suppress discovery/execution.
- Action limits are server-parsed, exact and bounded; write policies without any hard limit are invalid.
- Frequency receipts are retry-idempotent and durable across replicas.
- Secrets, credentials, refresh tokens and private keys are not returned by tools and prohibited capability names cannot be admitted.

### Repository checks

- Root `go test ./...`: PASS under temporary local Go 1.23.2 compatibility; canonical Go declaration is restored before packaging.
- Root `go vet ./...`: PASS.
- `go build -trimpath -buildvcs=false ./cmd/...`: PASS.
- Go formatting: PASS.
- Targeted race for Agent Governance + MCP + Approval/Audit + governance/approval repositories: PASS.
- Agent Governance + MCP repeat suite: 20/20 PASS.
- Architecture: PASS with 71 modules, 8 providers, 42 reviews and zero unreviewed changes.
- Migration catalog: PASS with 26 migrations, latest `000026`.
- New Draft-2020-12 AI policy/provenance/prompt-regression schemas: schema-valid; 5-case prompt-injection corpus validates and executes.
- Generated SDK drift/boundary: PASS with 29 public operations.
- Frontend static validation: PASS, 7/7 tests.
- Linux connector sandbox and generic conformance: PASS; Task 079 changes no provider contract.
- Canonical `make contracts` is environment-blocked because the nested contract tool requires Go >=1.26 while the sandbox exposes Go 1.23.2 with no pinned-toolchain download path. New Task-079 schemas are independently Draft-2020-12 validated.
- `make policy` reaches the same nested Go-1.26 supply-chain check and is environment-blocked after module verification for the same reason.

## Dependency boundary

Task `079` closes the repository AI-governance publication gate created by Task `018`. Production MCP exposure still requires Task `084 Enterprise IAM` trusted identity/control-plane wiring. The canonical next dependency-ready task is `020 Social Core`, followed by `019 n8n Node` and `078 Plugin Marketplace Governance` in the Phase-2 chain.
