# Kimi (Moonshot AI) capability audit

| Capability | Decision | Evidence boundary |
|---|---|---|
| `ai.completion.generate` | admitted | One bounded chat-completion call per request; tenant-approved prompt text only |
| streaming responses | rejected | Only the non-streaming Chat Completions shape is implemented |
| write to the provider's account/data | rejected | This connector only sends a prompt and reads back text |
| credentials | `api_key` | Stored via `secrets.ClassAIProviderCredential`; read only inside a host-owned `UseSecret` callback |

Remote non-2xx responses and empty/malformed completions are normalized to a bounded error; raw provider response bodies are never surfaced to the caller.
