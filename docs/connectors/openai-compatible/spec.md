# OpenAI-compatible connector specification

## Capability

- provider id: `openai-compatible`
- family: `ai`
- SDK: v1
- capability: `ai.completion.generate`
- authentication: `api_key` (Bearer), required
- default host: `api.openai.com`

## Transport contract

The provider receives a host-injected typed `Transport.Do(ctx, Request)` where `Request{Host, Path, Headers, Body}` carries an already-marshaled JSON Chat Completions payload. The provider builds `POST /v1/chat/completions` with `{model, messages:[{role, content}, ...]}` and parses `choices[0].message.content` from the response. Host resolution, TLS, DNS pinning and public-IP-only enforcement are entirely host-owned (`internal/platform/builtinruntime`); this package never imports `net/*`.

Account-level configuration may override the default host with any OpenAI-wire-compatible gateway (a bare hostname, never a full URL — this package cannot parse one).

## Security and privacy

The credential is read only inside `runtime.Secrets().UseSecret(...)` and is never logged or included in returned errors. The only tenant text that leaves TORGNEXA is the caller-assembled `system_prompt`/`prompt`; the response text is returned to the caller and is not persisted by this package.
