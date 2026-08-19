# Incident Management & Runbooks

Operational readiness requires predefined actions for predictable failures.

Minimum runbooks:
- PostgreSQL unavailable/read-only;
- Kafka broker/controller degradation and consumer lag;
- ClickHouse ingestion backlog;
- object storage failure;
- Keycloak/auth outage;
- connector credential expiry/revocation;
- marketplace throttling/outage;
- stuck outbox/inbox/DLQ;
- reconciliation drift spike;
- signing/EDO/ChZ failure;
- data leak/security incident;
- bad release/rollback.

Every incident has severity, owner, communication channel, timeline, evidence, remediation and postmortem follow-up.
