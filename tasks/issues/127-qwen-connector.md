# Task 127: Qwen (Alibaba Cloud) connector

## Status
`repository-complete` — 2026-08-20.

## Objective
Register Qwen (Alibaba Cloud) as a further `ai`-family provider for the Task-122 AI provider account capability, reusing DashScope's OpenAI-compatible mode wire format without importing `connectors/openai-compatible` or `connectors/kimi`.

## Dependencies
122

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Qwen is reachable only through the existing `/settings/ai-providers:analyze` operation; provider dispatch stays confined to `internal/platform/builtinruntime`; no connector-to-connector import.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- `qwen` is admitted through Connector SDK v1 with `ai.completion.generate` only, defaulting to host `dashscope.aliyuncs.com` with an account-configurable hostname override (for example to the international DashScope region);
- request/response marshaling is declared directly inside `connectors/qwen` (not imported from `connectors/openai-compatible` or `connectors/kimi`, since provider packages may not import each other); the completions path is DashScope's compatible-mode shape (`/compatible-mode/v1/chat/completions`), distinct from the plain `/v1/chat/completions` other OpenAI-compatible providers use;
- `internal/platform/builtinruntime.Registry.AICompletion` gains one additional `case "qwen"` dispatch arm;
- `ai_provider_accounts.provider` CHECK constraint widened to admit `qwen` (migration `000015_ai_provider_qwen_deepseek.sql`, verified applied against a live PostgreSQL 18 instance alongside the full active migration chain, 15/15);
- `AIProviderAccount`/`AIProviderAccountCreate`/`AIAnalyzeResponse` OpenAPI schemas and `AIProviderSettings.tsx`'s provider list/host-override field updated;
- deterministic fixtures/tests, `ARCH-127`, capability audit/spec docs and Task-064 conformance report are present.

## Qualification
Task-064 provider conformance: **13/13 PASS**, report SHA-256 `c191e2d768da6493986192256f63f88a0017de04d7470320d48cbe685253f80d`.
