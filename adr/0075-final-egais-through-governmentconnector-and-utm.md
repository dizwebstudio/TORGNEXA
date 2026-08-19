# ADR 0075: EGAIS through GovernmentConnector and UTM

## Status
Accepted

## Context
Task 083 needs a regulated EGAIS integration without introducing provider-specific Core behavior. The official integration boundary is the Universal Transport Module and remote tickets/status are authoritative.

## Decision
Admit an optional GovernmentConnector provider for EGAIS UTM with document read/write, inventory/reference reads and reconciliation. Writes require artifact, approval and idempotency references before transport invocation.

## Consequences
The capability becomes an explicit governed TORGNEXA boundary with deterministic failure semantics, test evidence and operator-visible state.

## Alternatives considered
Direct Core branching and browser automation were rejected. Persisting crypto private material in connector state was rejected in favor of Task 069 signing/secret boundaries.

## Compatibility impact
Connector SDK v1 root interfaces remain unchanged; only existing GovernmentConnector capabilities are implemented.

## Migration and data impact
Expand-only migration 000050 records append-only regulated remote evidence.

## Security and privacy impact
Credentials are opaque certificate references; regulated writes fail closed without approval/artifact/idempotency and remote state cannot be overridden locally.

## Operational impact
Deployments must operate qualified UTM/signing infrastructure and reconcile tickets/errors when remote outcomes are ambiguous.
