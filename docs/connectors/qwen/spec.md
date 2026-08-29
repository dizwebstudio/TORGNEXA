# Qwen (Alibaba Cloud) connector specification

## Capability

- provider id: `qwen`
- family: `ai`
- SDK: v1
- capability: `ai.completion.generate`
- authentication: `api_key` (Bearer), required
- default host: `dashscope.aliyuncs.com`

## Transport contract

Qwen is reached through DashScope's OpenAI-compatible mode (`/compatible-mode/v1/chat/completions`), which uses the same wire format OpenAI's Chat Completions API does. `connectors/ai/qwen` intentionally re-declares the request/response types rather than importing `connectors/ai/openai-compatible` or `connectors/ai/kimi`, since connector packages may not import each other (`architecture/policy.json` provider composition boundary). The provider receives a host-injected typed `Transport.Do(ctx, Request)`; host resolution, TLS, DNS pinning and public-IP-only enforcement are entirely host-owned. This package never imports `net/*`. An account may override the default host (for example to `dashscope-intl.aliyuncs.com` for the international region) the same way `openai-compatible` and `kimi` accounts do.

## Security and privacy

The credential is read only inside `runtime.Secrets().UseSecret(...)` and is never logged or included in returned errors. The only tenant text that leaves TORGNEXA is the caller-assembled `system_prompt`/`prompt`.
