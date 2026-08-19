# ADR 0080: CDEK as production reference logistics provider

## Status
Accepted

## Context
Task 090 must prove the generic Logistics SDK against a production carrier including asynchronous tracking and pickup points.

## Decision
Admit CDEK through existing rate, shipment, tracking, cancel, return, label and pickup-point interfaces. Remote tariff ids remain provider-local and are mapped to canonical service codes by Task 074 host mapping.

## Consequences
The capability becomes an explicit governed TORGNEXA boundary with deterministic failure semantics, test evidence and operator-visible state.

## Alternatives considered
Leaking carrier tariff ids into Core and direct provider branches were rejected. Storing OAuth client secrets in shipment records was rejected.

## Compatibility impact
Logistics SDK v1 remains unchanged; the provider is additive.

## Migration and data impact
No new provider-specific table is needed because Task 074/075 generic logistics and PUDO evidence is reused.

## Security and privacy impact
OAuth credentials stay behind SecretAccessor and async status replay is idempotent/reconciled.

## Operational impact
Operators monitor carrier webhooks/polling drift and maintain account-local service mappings.
