# Open WebUI capability audit

| Capability | Decision | Evidence boundary |
|---|---|---|
| `ai.completion.generate` | admitted | One bounded non-streaming gateway completion |
| streaming responses | rejected | No stream transport or incremental response contract |
| tool use / remote actions | rejected | Only the first text completion choice is returned |
| product/order synchronization | rejected | No commerce entity interfaces are implemented |
| credentials | `api_key` | Open WebUI token is callback-scoped and never exposed |

Non-2xx, malformed and empty responses are normalized to bounded connector
errors. The local transport blocks public destinations and redirects.
