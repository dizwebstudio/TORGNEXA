# Task 159 — Google Gemini and Grok AI providers

## Status

Repository implementation complete. Midjourney is intentionally not admitted
as a connector because its official policy does not provide a public API and
prohibits third-party automation.

## Scope

Add Google Gemini and xAI Grok to the existing tenant-scoped AI analytics
provider surface. Both providers must use callback-scoped API keys, the
host-owned HTTPS transport, one bounded non-streaming completion capability,
and the existing AI egress governance and audit path.

## Acceptance criteria

- Gemini uses `x-goog-api-key` and `POST /v1beta/models/{model}:generateContent`.
- Grok uses `Authorization: Bearer` and `POST /v1/chat/completions`.
- Provider manifests, runtime support, policy, OpenAPI enum, migration,
  generated catalogs and SDK artifacts agree.
- UI settings and report AI selector display Google Gemini and Grok in Russian.
- Connector tests and thirteen-check conformance evidence are present.
- No Midjourney browser automation, private endpoint, cookie or unofficial API
  is added.
