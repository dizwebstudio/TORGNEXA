# Task 085: Siem Security Event Export

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- minimized asynchronous SIEM queue/worker, four sink contracts, retry/DLQ and migration 000052.
- Architecture review `ARCH-085` and executable tests are included.

## Objective
Implement normalized SecurityEvent export to SIEM through pluggable syslog/TLS, signed webhook, Kafka and OTLP sinks.

## Dependencies
003, 007, 009, 063, 077, 084

## Deliverables
Security event schema, sink port, durable worker/retry/DLQ, redaction/minimization, sink health/lag metrics and fixtures.

## Acceptance
SIEM outage never blocks business commit; secrets/PII are minimized; replay is idempotent; security export is observable and tenant-safe.

Run required repository checks and report results, risks and follow-ups.