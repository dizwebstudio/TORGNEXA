# ADR 0097: AI provider settings capability with OpenAI-compatible as the first admitted `ai` provider

Status: Accepted

## Context

Operators need a tenant-scoped way to configure an external AI provider (API key/host/model) and to send a bounded, caller-assembled analytics prompt to it for a completion, without Core, App or the settings API ever branching on which provider is configured. Task 122 must therefore do two things in one reviewed change, exactly as ADR 0048 did for the first marketplace provider: introduce a new settings/API capability (tenant-scoped `ai_provider_accounts`, create/list/disable/analyze endpoints) and register the first concrete provider under that capability, `openai-compatible`, family `ai`, under `connectors/ai/openai-compatible`.

Leaving the capability without an admitted provider would ship dead settings UI; admitting a provider without the surrounding capability would give provider code a caller surface it does not own. Task 090's `internal/platform/builtinruntime` composition boundary already exists as the sole module allowed to branch on `ConnectorID`, so this task routes all provider dispatch through it rather than inventing a second exemption.

## Decision

Add `ai_provider_accounts` (migration `000012_ai_advisory.sql`, RLS forced, `DELETE`/`TRUNCATE` revoked, matching the `catalog_product_images` guard-trigger style) holding only label/model/base_url/folder_id/enabled/version plus a `secrets.Reference`; credential bytes are never stored in Postgres. Add four additive OpenAPI 0.16.0 operations under `/settings/ai-providers(:disable|:analyze)`, gated by `settings.ai_providers.read`/`write` (admin) for account management and a separate `ai.analyze` (admin/manager/operator) for triggering a completion.

Register `openai-compatible` as the first `ai`-family provider: `Family.Valid()` and `connectors.Capability` gain `ai` / `ai.completion.generate`; the connector package declares only `Manifest()`/`Health()` and a `Complete()` helper built on a host-injected `Request/Response/Transport` primitive — it imports no `net/*` package, matching the Task-025/029 boundary. `internal/platform/builtinruntime` gains the one sanctioned `switch account.ConnectorID` branch (`Registry.AICompletion`) that dispatches to the connector's `Complete`; the settings API and `internal/platform/aiadvisory` port never see a provider name in a conditional.

## Consequences

`openai-compatible` becomes the first architecture-registered `ai`-family provider and must retain canonical manifest/spec/capability-audit/conformance evidence, exactly like every other admitted provider. Kimi, GigaChat and YandexGPT follow as separate `new_provider` tasks (123/124/125) that reuse this capability and add only their own connector package plus a `builtinruntime.Registry.AICompletion` case; they do not reopen this ADR.

The audit trail for create/disable/analyze intentionally records only actor, correlation id and a bounded outcome summary (provider, label, ok), never the prompt or completion text, so the new capability cannot become an unbounded data-egress channel through audit storage itself.

## Alternatives considered

Branching on provider name inside `internal/app/api/ai_advisory.go` or `internal/platform/aiadvisory` was rejected: it would put provider-specific logic outside `connectors/`/`builtinruntime`, the same violation ADR 0090 closed for the first built-in composition boundary.

Giving each AI connector direct `net/http` access was rejected for the same reason Task 025/029 forbid it for every other provider: it would grant unmediated DNS/socket authority outside the DNS-pinned, public-IP-only host transport.

Storing the provider API key directly on the `ai_provider_accounts` row was rejected; credentials route exclusively through the existing `secrets.SecretProvider`/`secrets.Reference` boundary used by every other connector account.

## Compatibility impact

Frozen `sdk.Connector`/`sdk.Runtime` root interfaces are unchanged. `ai` is an additive `Family`; `ai.completion.generate` is an additive `Capability`. No existing OpenAPI operation is modified, only four new ones are added.

## Migration and data impact

Migration `000012_ai_advisory.sql` is a normal post-baseline expand migration adding `ai_provider_accounts`; the immutable `000001`-`000011` pre-v1 squash is unchanged and remains byte-identical to `scripts/generate-pre-v1-baseline.py`'s deterministic output.

## Security and privacy impact

Provider code holds no direct network, database, filesystem, process or Core/App authority; all HTTPS calls route through `builtinruntime`'s DNS-pinned, public-IP-only transport, and credential bytes are read only inside a host-owned `UseSecret` callback. The only tenant data that leaves TORGNEXA through this capability is the caller-assembled prompt at analyze-call time; the response is returned to the caller and is not itself persisted by this capability.

## Operational impact

Disabling an `ai_provider_accounts` row immediately stops further analyze calls for that account. Remote non-2xx responses and malformed completions normalize to a bounded 502 at the API layer. Task-064 conformance (13/13) is a release blocker for `openai-compatible` exactly as for every other provider.
