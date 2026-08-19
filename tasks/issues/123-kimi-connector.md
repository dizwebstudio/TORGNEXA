# Task 123: Kimi (Moonshot AI) connector

## Status
`repository-complete` — 2026-08-19.

## Objective
Register Kimi (Moonshot AI) as a second `ai`-family provider for the Task-122 AI provider account capability, reusing its OpenAI-compatible wire format without importing `connectors/openai-compatible`.

## Dependencies
122

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Kimi is reachable only through the existing `/settings/ai-providers:analyze` operation; provider dispatch stays confined to `internal/platform/builtinruntime`; no connector-to-connector import.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- `kimi` is admitted through Connector SDK v1 with `ai.completion.generate` only, defaulting to host `api.moonshot.ai` with an account-configurable hostname override;
- request/response marshaling is declared directly inside `connectors/kimi` (not imported from `connectors/openai-compatible`, since provider packages may not import each other);
- `internal/platform/builtinruntime.Registry.AICompletion` gains one additional `case "kimi"` dispatch arm;
- deterministic fixtures/tests, `ARCH-123`, capability audit/spec docs and Task-064 conformance report are present.

## Qualification
Task-064 provider conformance: **13/13 PASS**, report SHA-256 `2e95cdf24105604525fa8f7e57df5bfe70cf5c6863aed844f72a9f1d63e7868c`.
