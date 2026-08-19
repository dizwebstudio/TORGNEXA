# Task 085 contract v1

Normalized minimized SecurityEvent records are queued after audit and exported asynchronously to syslog/TLS, signed webhook, Kafka or OTLP sinks with retry/DLQ and lag health.

All tenant data is organization/workspace scoped, retry/idempotency semantics are explicit, and credentials/PII are minimized behind existing security boundaries.
