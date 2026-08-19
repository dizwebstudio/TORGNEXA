# SIEM Security Event Export

Normalized minimized SecurityEvent records are queued after audit and exported asynchronously to syslog/TLS, signed webhook, Kafka or OTLP sinks with retry/DLQ and lag health.
