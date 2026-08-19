# ADR 0027: Asynchronous Normalized SIEM Export

## Decision
Persist audit first, then export minimized normalized security events asynchronously through pluggable syslog/webhook/Kafka/OTLP sinks.

## Consequences
SIEM outage does not block transactions; export health/lag and DLQ become observable operational state.
