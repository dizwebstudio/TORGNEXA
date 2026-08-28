# LM Studio capability audit

| Capability | Decision | Evidence boundary |
|---|---|---|
| `ai.completion.generate` | admitted | One bounded non-streaming `/chat/completions` call |
| streaming responses | rejected | No streaming transport is exposed |
| tool use / remote actions | rejected | The response mapper returns text only |
| product/order synchronization | rejected | No commerce connector interfaces are implemented |
| credentials | `api_key` | SecretProvider callback only; no credential persistence in connector |

The host rejects public or arbitrary HTTP destinations and pins every approved
local call to a resolved private address.
