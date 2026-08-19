# Task 125: YandexGPT connector

## Status
`repository-complete` — 2026-08-19.

## Objective
Register YandexGPT as a fourth `ai`-family provider for the Task-122 AI provider account capability, using folder-scoped model URIs.

## Dependencies
122

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
YandexGPT is reachable only through the existing `/settings/ai-providers:analyze` operation; `Health()` satisfies the frozen `sdk.Connector` interface exactly (no FolderID parameter); provider dispatch stays confined to `internal/platform/builtinruntime`.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- `yandexgpt` is admitted through Connector SDK v1 with `ai.completion.generate` only, calling `llm.api.cloud.yandex.net` with a `gpt://<folder_id>/<model>` URI built from the account's `folder_id`;
- `Health(ctx, account, runtime)` matches the frozen 3-argument `sdk.Connector` boundary and reports account/manifest validity only; the live folder-scoped probe is a separate `HealthCheckWithFolder` reachable only through `builtinruntime`;
- `internal/platform/builtinruntime.Registry.AICompletion` gains one additional `case "yandexgpt"` dispatch arm;
- deterministic fixtures/tests, `ARCH-125`, capability audit/spec docs and Task-064 conformance report are present.

## Qualification
Task-064 provider conformance: **13/13 PASS**, report SHA-256 `159ff1db17f6f1f478ba2d10f6b61dfda152a6295c6787b0cc7d5d687d55c28b`.
