# Open WebUI connector specification

## Capability

- provider id: `open-webui`
- display name: Open WebUI
- family: `ai`
- SDK: v1
- capability: `ai.completion.generate`
- authentication: `api_key` bearer token, required
- default base URL: `http://open-webui:3000/api`
- endpoint: `POST /chat/completions`

The adapter uses Open WebUI's OpenAI-compatible gateway contract for one
bounded, non-streaming completion. The base path is configurable only when it
passes the local endpoint allowlist.

## Transport boundary

Open WebUI is reached only through the host-owned local AI transport. The host
performs DNS resolution, private-address validation and pinned dialing, then
applies timeout, proxy, redirect and response-size policy. Arbitrary LAN and
public HTTP destinations are rejected.

## Security and privacy

The bearer token is read through the callback-scoped SecretProvider path and is
never logged or persisted by the connector. Prompt text is caller-approved
analysis input; responses are returned to that request and not stored here.
Streaming, tools, remote actions and commerce synchronization are not admitted.
