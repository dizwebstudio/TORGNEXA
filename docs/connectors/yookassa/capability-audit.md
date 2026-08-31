# Capability audit

Only capabilities demonstrated by the current official interface and Connector SDK v1 are admitted. POST operations are bound to a caller-supplied Idempotence-Key (max 64 characters). HTTP 500 is treated as outcome-unknown and must be reconciled with the same key or GET. Full and partial refunds remain exact minor-unit operations. Webhook receipt is admitted because YooKassa documents HTTP notifications and recommends checking the current object status; TORGNEXA performs that re-fetch and deduplicates the verified delivery before changing local state. No browser-cookie automation, private editor endpoints, raw card credentials, or provider-specific Core branches are permitted.

Official documentation: https://yookassa.ru/developers/api
