# ADR 0077: Asynchronous minimized SIEM export

## Status
Accepted

## Context
Task 085 must export normalized security evidence without allowing a SIEM outage to block commerce commits or leak secrets/PII.

## Decision
Queue minimized SecurityEvent records after authoritative audit, then deliver through pluggable syslog/TLS, signed webhook, Kafka or OTLP sinks with bounded retry and DLQ.

## Consequences
The capability becomes an explicit governed TORGNEXA boundary with deterministic failure semantics, test evidence and operator-visible state.

## Alternatives considered
Synchronous SIEM calls on business transactions were rejected. Exporting raw audit payloads was rejected because of privacy/secrets risk.

## Compatibility impact
No existing business API/event contract changes; SIEM is an additive asynchronous projection.

## Migration and data impact
Expand-only migration 000052 adds tenant SIEM queue and append-only DLQ evidence.

## Security and privacy impact
Event validation rejects secret-like attributes; actor identity is a fingerprint and signed-webhook bodies use HMAC at the sink boundary.

## Operational impact
SRE monitors queue lag, sink health and DLQ growth; outages degrade export only, not business transactions.
