# Customer Service Unified Inbox

Task `057` implementation lives in `internal/platform/customerinbox`.

## Safety invariants

Unified inbox identity is deduplicated by provider/account/remote thread, message bodies are PII-minimized before persistence, replies are account-scoped, and AI output is draft-only by default.

## Persistence

PostgreSQL expand migration: `000036_customer_service_inbox.sql`. In-memory implementations in tests are reference semantics, not production durability.
