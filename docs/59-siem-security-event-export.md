# SIEM / Security Event Export

TORGNEXA has an immutable application audit trail, but enterprise operators also require reliable export of normalized security events to external SIEM/SOC tooling.

## Security event model

A normalized event contains event ID/time, tenant/workspace, actor type/id, source IP/proxy context, authentication/session reference, action, resource, outcome, risk class, correlation/causation IDs and minimized metadata. Secrets and sensitive payload bodies are prohibited.

## Sinks

Implement an asynchronous `SecurityEventSink` abstraction with adapters for:
- RFC5424 syslog/TLS;
- signed webhook;
- dedicated Kafka topic;
- OpenTelemetry Logs/OTLP;
- optional CEF/LEEF mapping at the adapter edge.

Audit persistence is primary; SIEM delivery failure must not block the business transaction. Export uses durable retry/DLQ and health/lag metrics.

## Events of interest

Authentication/MFA/session changes, privilege/role changes, service-account/API-key lifecycle, secret access/rotation, approval/signing actions, connector credential changes, bulk writes, data exports/privacy requests, plugin install/permission changes, security policy violations and edge/WAF signals.
