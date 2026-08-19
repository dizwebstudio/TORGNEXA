# Task 057 — Customer Service Unified Inbox contract v1

Status: repository-qualified.

Unified inbox identity is deduplicated by provider/account/remote thread, message bodies are PII-minimized before persistence, replies are account-scoped, and AI output is draft-only by default.

All monetary values use integer minor units plus ISO-shaped currency; tenant scope is mandatory; retries preserve canonical idempotency identity.
