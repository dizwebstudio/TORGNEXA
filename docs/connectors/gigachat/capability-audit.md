# GigaChat (Sber) capability audit

| Capability | Decision | Evidence boundary |
|---|---|---|
| `ai.completion.generate` | admitted | One OAuth exchange plus one bounded chat-completion call per request |
| token caching/reuse across requests | rejected | Each `Complete` call performs its own OAuth exchange; no token is persisted |
| write to the provider's account/data | rejected | This connector only sends a prompt and reads back text |
| credentials | `basic` | Stored via `secrets.ClassAIProviderCredential`; read only inside a host-owned `UseSecret` callback |

Remote non-2xx responses (both OAuth and completion stages) and empty/malformed completions are normalized to a bounded error; raw provider response bodies are never surfaced to the caller.
