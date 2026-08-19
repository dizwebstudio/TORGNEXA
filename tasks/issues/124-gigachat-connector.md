# Task 124: GigaChat (Sber) connector

## Status
`repository-complete` — 2026-08-19.

## Objective
Register GigaChat (Sber) as a third `ai`-family provider for the Task-122 AI provider account capability, performing a per-call OAuth exchange before completion.

## Dependencies
122

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
GigaChat is reachable only through the existing `/settings/ai-providers:analyze` operation; both the OAuth exchange and the completion call are host-mediated; provider dispatch stays confined to `internal/platform/builtinruntime`.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- `gigachat` is admitted through Connector SDK v1 with `ai.completion.generate` only; `Complete()` performs a two-request sequence, OAuth token exchange against `ngw.devices.sberbank.ru` (form-encoded `scope=GIGACHAT_API_PERS`) followed by a Bearer-token completion call against `gigachat.devices.sberbank.ru`;
- the exchanged OAuth token is used once per call and never persisted;
- `internal/platform/builtinruntime.Registry.AICompletion` gains one additional `case "gigachat"` dispatch arm;
- deterministic fixtures/tests, `ARCH-124`, capability audit/spec docs and Task-064 conformance report are present.

## Qualification
Task-064 provider conformance: **13/13 PASS**, report SHA-256 `793efb04a50da0484bee9f087d891eca7da2105911db53958af1ad3ba21c57d2`.
