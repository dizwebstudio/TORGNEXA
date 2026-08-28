# Task 141: Claude AI provider

## Status

`repository-complete` — 2026-08-28.

## Objective

Add Claude (Anthropic) to the tenant-scoped AI provider settings and analysis
surface with a host-mediated, production-safe Connector SDK adapter.

## Dependencies

122, 130, 134

## Acceptance

- Claude is selectable in Settings → AI providers and in report analysis;
- account creation, governance, audit and secret handling reuse the existing
  `/api/v1/settings/ai-providers` boundary;
- `internal/platform/builtinruntime.Registry.AICompletion` is the only
  provider dispatch point and the connector has no direct network authority;
- Anthropic Messages API requests use `x-api-key`, the version header and a
  bounded text-only response;
- OpenAPI, generated SDKs, runtime catalog, migration metadata and docs remain
  synchronized and all repository gates pass.

## Implementation evidence

- `connectors/claude` implements the SDK v1 manifest, Messages request/response
  mapping, health normalization, deterministic fixtures and conformance
  candidate;
- migration `000020_ai_provider_claude.sql` widens the provider allow-list;
- the frontend settings and report surfaces label Claude as `Claude
  (Anthropic)` and support an optional HTTPS Base URL proxy;
- `ARCH-141`, capability audit/specification and conformance evidence are
  retained under the architecture/docs trees.

## Qualification

Focused connector/runtime/migration tests pass. Live Anthropic qualification is
left as an external gate because this repository has no provider API key or
non-production tenant.
