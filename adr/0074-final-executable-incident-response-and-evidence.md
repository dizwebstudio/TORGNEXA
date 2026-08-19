# ADR 0074: Executable incident response and evidence

## Status
Accepted

## Context
Task 077 needs incident response that operators and automation can execute consistently after Task 066 SLO triggers. Markdown-only advice cannot prove rollback or evidence capture.

## Decision
Represent incident signals, state, runbooks and steps as validated typed data. Execute only host-owned safe actions; every step requires validation, rollback and evidence and failure triggers reverse rollback attempts.

## Consequences
The capability becomes an explicit governed TORGNEXA boundary with deterministic failure semantics, test evidence and operator-visible state.

## Alternatives considered
Free-form wiki runbooks were rejected because required rollback/evidence fields cannot be enforced. Fully autonomous remediation was rejected because privileged actions require bounded operator policy.

## Compatibility impact
The change is additive and does not change public API or event schemas. Existing SLO signals can be mapped without changing their payloads.

## Migration and data impact
Expand-only migration 000049 stores tenant incidents and append-only evidence. Existing tables remain readable/writable by old binaries.

## Security and privacy impact
Runbook actions are identifiers resolved by trusted host executors; credentials and raw payloads are excluded from incident evidence.

## Operational impact
Operators gain deterministic runbooks for DB, Kafka, auth, storage, connector, DLQ, reconciliation, signing and security failures with auditable outcomes.
