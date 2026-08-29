# Ollama connector specification

## Capability

- provider id: `ollama`
- display name: Ollama
- family: `ai`
- SDK: v1
- capability: `ai.completion.generate`
- authentication: `api_key` contract, required (a non-empty placeholder is
  acceptable when the local server has authentication disabled)
- default base URL: `http://ollama:11434/v1`
- endpoint: `POST /chat/completions`

Wire format: Ollama's OpenAI-compatible API. The connector sends one bounded,
non-streaming chat completion and accepts a response with a non-empty first
choice message.

## Transport boundary

`connectors/ai/ollama` owns only typed request/response mapping. DNS, private-IP
resolution, pinned dialing, timeouts, redirects, proxy suppression and response
limits are enforced by `internal/platform/builtinruntime/local_ai_transport.go`.
Only the explicit local host allowlist is admitted; external providers remain
on the public HTTPS transport.

## Security and privacy

The credential is read only inside `runtime.Secrets().UseSecret(...)`. Prompt
text and credentials are not persisted or logged by the connector. The
connector does not claim product synchronization, streaming, tool use or
provider-side writes.
