# Claude connector specification

## Capability

- provider id: `claude`
- display name: Claude (Anthropic)
- family: `ai`
- SDK: v1
- capability: `ai.completion.generate`
- authentication: `api_key`, required
- default host: `api.anthropic.com`
- endpoint: `POST /v1/messages`

Wire reference: [Anthropic Messages API](https://docs.anthropic.com/en/api/messages).

## Transport contract

`connectors/claude` implements Anthropic's non-streaming Messages API shape
directly. It sends `model`, a bounded `max_tokens` value, an optional top-level
`system` prompt and one user message. The response is accepted only when it
contains a non-empty `content` block with `type: text`; tool use, streaming and
other content types remain outside this admission.

The typed `Transport.Do` boundary is host-mediated. DNS resolution, TLS,
public-IP/SSRF checks, redirects, timeouts and response-size limits belong to
`internal/platform/builtinruntime`; this package does not import `net/*` or
perform socket I/O. An account may provide a validated HTTPS Base URL proxy,
which is reduced to a bare hostname by the host before dispatch.

## Security and privacy

The API key is read only inside `runtime.Secrets().UseSecret(...)` and is sent
in Anthropic's `x-api-key` header. It is never logged, persisted by the
connector, included in an error, or returned to the caller. The only tenant
content sent remotely is the caller-approved system/user prompt from the
governed AI analysis operation.

The connector does not persist responses, create provider-side resources or
claim generic commerce/product synchronization.
