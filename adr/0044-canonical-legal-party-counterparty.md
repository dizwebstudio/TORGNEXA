# ADR 0044: Canonical legal-party and counterparty master

Status: Accepted

## Context
ERP, EDO/MChD, payments, procurement and compliance require the same legal identities, bank details, contracts and authority references. Without one canonical master, each downstream module would copy INN/KPP/OGRN, legal names and counterparty identifiers, making reconciliation and privacy controls unreliable.

## Decision
Task 081 adds `internal/core/legalparty` and PostgreSQL adapter `internal/platform/postgres/legalpartyrepo`. LegalEntity, IndividualEntrepreneur and Branch are party masters; Counterparty is a tenant business role referencing one party. Addresses, bank accounts, contracts and authority references are versioned subordinate masters. Russian identifiers are validated by a typed country adapter while the aggregate model remains country-neutral.

Generic `connector_entity_mappings` is broadened additively in mapping v4. Remote ERP/EDO/provider identifiers never enter Core party structs. Search is tenant-scoped and bounded. Duplicate evidence is explainable; merge preview is deterministic/fingerprinted and non-executing.

All legal-party mutations commit Audit, Outbox and Lineage evidence in the same PostgreSQL transaction. Evidence contains canonical identity/version/change, not names, tax IDs, bank numbers or credentials.

## Consequences
Task 015/016 ERP connectors, Task 069/070 MChD/EDO, Task 052 procurement, payment tasks and Task 082 compliance can reference stable party/counterparty IDs. IndividualEntrepreneur data is explicitly privacy-sensitive and must remain behind authenticated tenant scope. Destructive merge remains a separate approved workflow.

## Alternatives considered
Duplicating legal details in each downstream module was rejected because it creates conflicting masters and privacy drift. Treating every external ERP record as canonical was rejected because providers must remain projections behind connector mappings. Auto-merging exact-INN candidates was rejected because legal identity conflicts require review and approval.

## Compatibility impact
Additive domain/API/contracts and migration. Existing connector mapping v1-v3 remain published unchanged; v4 adds legal-party entity types. One new versioned event is added.

## Migration and data impact
Expand migration `000016_legal_party_counterparty.sql` creates new tenant-scoped master/history tables and broadens only the connector mapping type constraint/guard. No existing column is renamed or dropped.

## Security and privacy impact
Forced RLS and same-tenant reference guards prevent cross-tenant references. Normal hard delete/truncate is blocked. Individual-entrepreneur names and identifiers may be PII; audit/outbox/lineage payloads deliberately omit those values. Search is authenticated and `no-store`.

## Operational impact
Support and reconciliation can trace a counterparty from canonical party identity through external mappings, contracts and authority references. Backup/restore and migration rehearsals must include the new tables. Search indexes are bounded to canonical text/identifier fields.
