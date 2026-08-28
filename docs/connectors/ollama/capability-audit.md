# Ollama capability audit

| Capability | Decision | Evidence boundary |
|---|---|---|
| `ai.completion.generate` | admitted | One bounded non-streaming `/chat/completions` call |
| streaming responses | rejected | No stream transport or incremental response contract |
| tool use / remote actions | rejected | Only text from the first completion choice is returned |
| product/order synchronization | rejected | No commerce entity reader or writer is implemented |
| credentials | `api_key` | SecretProvider callback only; never returned or logged |

Malformed JSON, empty choices and non-2xx responses are normalized to bounded
errors. Public or arbitrary HTTP destinations cannot pass the host allowlist.
