# DeepSeek connector specification

## Capability

- provider id: `deepseek`
- family: `ai`
- SDK: v1
- capability: `ai.completion.generate`
- authentication: `api_key` (Bearer), required
- default host: `api.deepseek.com`

## Transport contract

DeepSeek's Chat Completions wire format is OpenAI-compatible. `connectors/ai/deepseek` intentionally re-declares the request/response types rather than importing `connectors/ai/openai-compatible`, since connector packages may not import each other (`architecture/policy.json` provider composition boundary). The provider receives a host-injected typed `Transport.Do(ctx, Request)`; host resolution, TLS, DNS pinning and public-IP-only enforcement are entirely host-owned. This package never imports `net/*`.

## Security and privacy

The credential is read only inside `runtime.Secrets().UseSecret(...)` and is never logged or included in returned errors. The only tenant text that leaves TORGNEXA is the caller-assembled `system_prompt`/`prompt`.
