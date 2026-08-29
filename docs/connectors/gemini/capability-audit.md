# Google Gemini capability audit

| Capability | Decision | Evidence boundary |
|---|---|---|
| `ai.completion.generate` | admitted | One bounded non-streaming `generateContent` request |
| streaming, tools, image/audio output | rejected | Not exposed by the provider-neutral analytics contract |
| credentials | `api_key` | Stored in `secrets.ClassAIProviderCredential` and read only in a callback-scoped secret accessor |

Only the governed, redacted analytics prompt leaves TORGNEXA. Provider
responses and credentials are not persisted or logged.
