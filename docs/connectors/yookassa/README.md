# YooKassa

Task reference connector. Reference acquiring connector proving create/status/refund/webhook/reconciliation with provider idempotency and no PAN/CVV handling. The runtime admits the webhook receiver: it parses the notification, re-fetches the payment from YooKassa, and applies only the returned authoritative status; byte-identical deliveries are deduplicated before the local payment transition.

Official documentation: https://yookassa.ru/developers/api
