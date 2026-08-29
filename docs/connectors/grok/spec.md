# Grok (xAI) connector specification

- provider id: `grok`
- display name: Grok (xAI)
- family: `ai`
- SDK: v1
- capability: `ai.completion.generate`
- authentication: xAI API key, required
- default host: `api.x.ai`
- endpoint: `POST /v1/chat/completions`

The adapter uses xAI's bounded, non-streaming Chat Completions shape with a
Bearer API key. An HTTPS host override may be supplied for a reviewed gateway.
See the [official xAI chat API documentation](https://docs.x.ai/developers/model-capabilities/legacy/chat-completions).

DNS resolution, TLS, public-IP/SSRF checks, redirects, timeouts and response
limits remain host-owned. The provider package performs no socket I/O.
