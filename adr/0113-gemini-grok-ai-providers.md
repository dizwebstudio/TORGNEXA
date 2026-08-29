# ADR-0113 — Google Gemini and Grok AI providers

## Context

TORGNEXA already exposes tenant-scoped AI provider accounts for governed
analytics completion. Users need Google Gemini and xAI Grok in the same surface.
Google documents Gemini's `generateContent` REST API with an `x-goog-api-key`
header; xAI documents a Bearer-authenticated Chat Completions API. Midjourney
does not publish a general API and prohibits third-party automation.

## Decision

Admit `gemini` and `grok` as SDK v1 providers with only
`ai.completion.generate`. Keep network policy, DNS/TLS, limits and secret
access in the existing host/runtime and governance boundary. Do not admit
Midjourney until the provider publishes an authorized API and its terms permit
this use.

## Consequences

The settings UI and report assistant can use Gemini and Grok with the existing
AI egress policies. Streaming, tools and image generation remain outside the
provider-neutral analytics contract. Midjourney is documented as unavailable
rather than represented by a misleading connector card.

## Security and privacy impact

API keys remain in `SecretProvider` and are read only during a governed
completion. Only redacted, approved analytics prompts leave the tenant. The
host transport enforces HTTPS, public destination checks, timeouts and bounded
responses.

## Compatibility impact

The OpenAPI provider enum, PostgreSQL CHECK constraint and generated SDK
metadata are widened additively. Existing provider accounts and clients remain
valid.

## Migration and data impact

Migration `000022_ai_provider_gemini_grok.sql` is an expand-only allow-list
change; no rows are rewritten and no secret material is migrated.

## Operational impact

Deployments need a Gemini API key or xAI API key per tenant account. Health and
completion failures use the existing normalized AI error/audit path.

## Alternatives considered

- Treat Gemini or Grok as generic OpenAI-compatible: rejected because Gemini's
  request/response contract is different and provider identity must remain
  explicit at the connector boundary.
- Automate Midjourney through the website or an unofficial API: rejected by
  the provider's terms and by the connector policy.
