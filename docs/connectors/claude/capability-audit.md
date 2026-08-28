# Claude capability audit

| Capability | Decision | Evidence boundary |
|---|---|---|
| `ai.completion.generate` | admitted | One bounded, non-streaming text completion through `/v1/messages` |
| streaming responses | rejected | No stream transport or incremental response contract is exposed |
| tool use / remote actions | rejected | Non-text content blocks are not returned as a completion |
| write to provider account/data | rejected | The adapter only sends a prompt and reads text |
| credentials | `api_key` | Stored via `secrets.ClassAIProviderCredential`; read only inside a host-owned callback |

Remote non-2xx responses, malformed JSON and responses without a non-empty text
block are normalized to bounded errors. Raw Anthropic response bodies never
reach the API, audit records or logs.
