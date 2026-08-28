# LM Studio connector specification

## Capability

- provider id: `lm-studio`
- display name: LM Studio
- family: `ai`
- SDK: v1
- capability: `ai.completion.generate`
- authentication: `api_key` contract, required (a non-empty placeholder is
  acceptable when LM Studio authentication is disabled)
- default base URL: `http://host.docker.internal:1234/v1`
- endpoint: `POST /chat/completions`

The adapter uses LM Studio's OpenAI-compatible request and response shape and
returns one non-streaming text completion.

## Transport boundary

The connector has no socket or DNS access. The reviewed builtin runtime owns
private/loopback resolution, pinned dialing, HTTP policy, timeouts, redirect
handling and response-size limits. Only `host.docker.internal`, the local
service names and loopback hosts are accepted for local HTTP URLs.

## Security and privacy

Credentials are available only inside the SecretProvider callback. Prompts,
responses and tokens are not persisted or logged by this adapter. Streaming,
tools, remote actions and commerce synchronization are outside the admitted
capability.
