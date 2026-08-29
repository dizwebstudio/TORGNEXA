# Google Gemini connector specification

- provider id: `gemini`
- display name: Google Gemini
- family: `ai`
- SDK: v1
- capability: `ai.completion.generate`
- authentication: Google Gemini API key, required
- endpoint: `POST /v1beta/models/{model}:generateContent`
- host: `generativelanguage.googleapis.com`

The adapter uses Google's REST `generateContent` contract with a bounded text
prompt, optional system instruction, and a non-streaming response. The key is
sent in `x-goog-api-key`; it is never placed in a URL or returned to callers.
See the [official Gemini API reference](https://ai.google.dev/api).

DNS resolution, TLS, public-IP/SSRF checks, redirects, timeouts and response
limits remain host-owned. The provider package performs no socket I/O.
