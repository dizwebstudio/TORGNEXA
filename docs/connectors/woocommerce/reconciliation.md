# WooCommerce reconciliation

WooCommerce remains remote-authoritative for connector-visible state. Full product/order scans plus signed product/order webhooks feed Task-013/014 synchronization and drift detection.

Create-product ambiguity is reconciled by exact seller SKU. Price, managed inventory and order-status ambiguity is reconciled by exact GET of the affected remote object. A network/5xx result that cannot be proven by read-after-write is `write_outcome_unknown` and is not blindly repeated.

Webhook reconciliation stores only the minimized verified envelope after replay claim; raw WooCommerce webhook bodies are request-scoped and must not become canonical commerce events.
