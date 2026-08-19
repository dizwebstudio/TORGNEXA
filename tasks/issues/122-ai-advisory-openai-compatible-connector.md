# Task 122: AI provider settings and OpenAI-compatible connector

## Status
`repository-complete` — 2026-08-19.

## Objective
Add a tenant-scoped settings capability to configure an external AI provider account (label/model/base_url/credential) and send a bounded analytics prompt to it for a completion, with `openai-compatible` as the first admitted `ai`-family provider.

## Dependencies
090

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Account CRUD is tenant-scoped and RLS-forced; credential bytes never reach Postgres; analyze is capability-gated separately from account management; provider dispatch is confined to `internal/platform/builtinruntime`.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- migration `000012_ai_advisory.sql` adds `ai_provider_accounts` (organization_id/workspace_id, RLS forced, `DELETE`/`TRUNCATE` revoked) storing only label/model/base_url/folder_id/enabled/version plus a `secrets.Reference`;
- four additive OpenAPI 0.16.0 operations under `/settings/ai-providers(:disable|:analyze)`, gated by `settings.ai_providers.read`/`write` (admin) and a separate `ai.analyze` (admin/manager/operator);
- `internal/platform/aiadvisory` is a non-branching port (`ValidateCreate`, `ValidateCompletionRequest`); it carries no provider-name conditional;
- `openai-compatible` is admitted through Connector SDK v1 with `ai.completion.generate` only; the connector package imports no `net/*` package and calls out through a host-injected `Request/Response/Transport` primitive;
- `internal/platform/builtinruntime.Registry.AICompletion` is the sole `switch account.ConnectorID` dispatch point for this capability;
- create/disable/analyze are audited as write_sensitive with a bounded summary (provider, label, ok) that excludes prompt/response text;
- deterministic fixtures/tests, `ARCH-122`, ADR 0097, capability audit/spec docs and Task-064 conformance report are present.

## Qualification
Task-064 provider conformance: **13/13 PASS**, report SHA-256 `187239dc9ca1aba351030c3e1ee18447e6d4f45b26426de9507f01dc0676b19c`.
