# YandexGPT capability audit

| Capability | Decision | Evidence boundary |
|---|---|---|
| `ai.completion.generate` | admitted | One bounded folder-scoped completion call per request |
| completion without a configured FolderID | rejected | `Complete`/`HealthCheckWithFolder` fail closed when FolderID is empty; no implicit default folder |
| write to the provider's account/data | rejected | This connector only sends a prompt and reads back text |
| credentials | `api_key` | Stored via `secrets.ClassAIProviderCredential`; read only inside a host-owned `UseSecret` callback |

Remote non-2xx responses and empty/malformed completions are normalized to a bounded error; raw provider response bodies are never surfaced to the caller.
