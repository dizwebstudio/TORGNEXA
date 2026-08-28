# ADR 0107: Claude AI provider admission

Status: Accepted

## Context

The AI-provider settings surface already supports tenant-scoped credentials,
governance, audit and bounded analysis for six providers. Claude (Anthropic)
was missing from the selectable provider list even though the runtime has a
stable provider-neutral completion boundary. Leaving it absent makes the
catalog and the supported AI capability inconsistent with the product request.

## Decision

Admit `claude` as a separate `ai_providers` surface with the single capability
`ai.completion.generate`. The connector implements Anthropic's non-streaming
Messages API (`POST /v1/messages`) directly and sends one user message, an
optional system prompt and a bounded `max_tokens` value. Only a non-empty
`content` block of type `text` is returned; streaming, tool use and remote
resource writes remain unadvertised.

The built-in registry composes Claude through the existing host-owned HTTPS
transport. The default host is `api.anthropic.com`; a tenant may provide a
validated HTTPS Base URL proxy, represented at dispatch as a bare hostname.
The API key is read only through the callback-scoped SecretProvider and is sent
as `x-api-key` together with `anthropic-version: 2023-06-01`. Core, API and
worker code remain provider-neutral.

## Consequences

Claude appears in Settings → AI providers and can be selected for governed
report analysis. The runtime catalog now has 40 manifests, 11 generic product
integrations, 14 separate-surface providers and 15 planned entries. Existing
AI accounts and product synchronization are unchanged.

## Compatibility impact

The existing AI-provider endpoints are reused. The provider enum in the
OpenAPI schemas and generated SDKs is widened additively; no endpoint or event
shape is removed. The frontend catalog and report provider labels gain one
entry.

## Migration and data impact

Migration `000020_ai_provider_claude.sql` widens the
`ai_provider_accounts.provider` check constraint. No rows, secret references,
events or backfills are rewritten.

## Security and privacy impact

The connector has no direct network or secret-store authority. Host DNS,
TLS, public-IP/SSRF checks, redirects, timeouts and response bounds remain in
the common transport. API keys never enter logs, errors, events, normal
database columns or frontend responses. Prompt egress remains subject to the
existing AI governance, data-class and audit controls.

## Operational impact

Provider failures are normalized to the existing unavailable/error paths and
the manifest supplies bounded timeout, concurrency and retry metadata.
Disabling the tenant account immediately prevents further analysis calls.
Live Anthropic qualification still requires a non-production API key and
retained remote health/completion evidence.

## Alternatives considered

Treating Claude as a generic commerce integration was rejected because it has
no product-sync bridge. Reusing an OpenAI-compatible connector was rejected
because Anthropic's authentication and Messages response shape differ. Adding
provider branches to Core or the worker was rejected; composition remains
isolated in `internal/platform/builtinruntime`.
