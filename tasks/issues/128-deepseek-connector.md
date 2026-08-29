# Task 128: DeepSeek connector

## Status
`repository-complete` — 2026-08-20.

## Objective
Register DeepSeek as a further `ai`-family provider for the Task-122 AI provider account capability, reusing its OpenAI-compatible wire format without importing `connectors/ai/openai-compatible` or `connectors/ai/kimi`.

## Dependencies
122

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
DeepSeek is reachable only through the existing `/settings/ai-providers:analyze` operation; provider dispatch stays confined to `internal/platform/builtinruntime`; no connector-to-connector import.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- `deepseek` is admitted through Connector SDK v1 with `ai.completion.generate` only, defaulting to host `api.deepseek.com` with an account-configurable hostname override;
- request/response marshaling is declared directly inside `connectors/ai/deepseek` (not imported from `connectors/ai/openai-compatible` or `connectors/ai/kimi`, since provider packages may not import each other);
- `internal/platform/builtinruntime.Registry.AICompletion` gains one additional `case "deepseek"` dispatch arm;
- `ai_provider_accounts.provider` CHECK constraint widened to admit `deepseek` alongside `qwen` in the same migration (`000015_ai_provider_qwen_deepseek.sql`, verified applied against a live PostgreSQL 18 instance alongside the full active migration chain, 15/15);
- `AIProviderAccount`/`AIProviderAccountCreate`/`AIAnalyzeResponse` OpenAPI schemas and `AIProviderSettings.tsx`'s provider list/host-override field updated;
- deterministic fixtures/tests, `ARCH-128`, capability audit/spec docs and Task-064 conformance report are present.

## Qualification
Task-064 provider conformance: **13/13 PASS**, report SHA-256 `d05879e64f5290694ec1e5f217136663981e453047cdd7aa4d5d65ed32a952c5`.
