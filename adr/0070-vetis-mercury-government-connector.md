# ADR 0070: VetIS Mercury government connector

## Status
Accepted — Task 70.

## Context
Task 072 needs veterinary document and stock integration with Mercury/VetIS while keeping government workflow status authoritative and preventing unapproved regulated writes.

## Decision
1. Admit `vetis-mercury` through Connector SDK government document/inventory/reconciliation interfaces.
2. Allow document writes only when an explicit approval reference is supplied.
3. Treat remote document/status/inventory observations as authoritative reconciliation input.
4. Persist append-only reconciliation evidence under tenant scope.

## Alternatives considered
- Direct provider SDK imports into Core: rejected.
- Unapproved automatic document writes: rejected.
- Scraping web UI/private RPC: rejected.

## Compatibility impact
The connector is additive and does not alter generic catalog/order contracts. Connector SDK root interfaces remain unchanged.

## Migration and data impact
Migration `000045` adds tenant-scoped append-only VetIS reconciliation evidence only; existing schemas are untouched.

## Operational impact
Production transport must be configured against an approved VetIS integration account, with credential rotation, rate/error monitoring and reconciliation schedules.

## Security and privacy impact
Credentials remain host-owned. Regulated write calls require approval references and provider code cannot access SQL/Core directly.

## Consequences
TORGNEXA can read/reconcile Mercury documents and inventory and perform explicitly approved writes through a typed boundary.
