# Task 073 — Payments/SBP provider SDK

Connector SDK v1 gains additive payment create/status/refund/reconcile/webhook interfaces. `sbp` is the baseline provider. It models merchant QR/link payment references and never accepts or stores PAN, CVV, track data or other raw card credentials.

Creation and refund requests require idempotency keys. Status/commission comes from the remote provider. Webhooks are accepted only after provider/transport signature verification; TORGNEXA persists a body digest and delivery ID for replay protection, never an authentication secret.

NSPK publishes an official SBP API for participants/merchant agents and documents API-based business payment acceptance. Production transport is bound to the acquiring participant's official contract. Official reference: `https://sbp.nspk.ru/api/`.
