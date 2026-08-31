# YooKassa Connector Spec

Family: `payment`. Reference acquiring connector proving create/status/refund/webhook/reconciliation with provider idempotency and no PAN/CVV handling.

Production networking is host-injected through a typed transport; provider code has no direct Core or SQL authority. POST operations are bound to a caller-supplied Idempotence-Key (max 64 characters). HTTP 500 is treated as outcome-unknown and must be reconciled with the same key or GET. Full and partial refunds remain exact minor-unit operations. The admitted webhook route accepts YooKassa HTTP notifications, re-fetches the payment by ID through the authenticated API, and uses only that authoritative status; the raw notification status is never trusted.

Official documentation: https://yookassa.ru/developers/api
