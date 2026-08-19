# Task 081: Legal Entity Counterparty Core

## Status
Repository implementation: **Completed** on 2026-08-10.

## Objective
Implement canonical LegalEntity/IndividualEntrepreneur/Branch/Counterparty/BankAccount/Contract/AuthorityReference master data used by ERP, EDO, MChD, payments, procurement and compliance.

## Deliverables
- [x] Provider-neutral LegalEntity, IndividualEntrepreneur, Branch and Counterparty masters.
- [x] Typed Russian INN/KPP/OGRN/OGRNIP checksum/pattern adapter.
- [x] Versioned addresses, bank accounts, contracts and authority references.
- [x] Tenant-scoped bounded API/search by name, INN and registration identity.
- [x] Explainable duplicate candidate logic and deterministic non-executing merge preview.
- [x] Generic connector mapping v4 for party/counterparty/contract/authority identities without downstream IDs in Core.
- [x] Forced-RLS PostgreSQL storage, same-tenant reference guards, lifecycle/version guards and no hard-delete/truncate paths.
- [x] Atomic Task-003 Audit + Task-008 Outbox + Task-030 Lineage evidence for mutations.
- [x] Draft 2020-12 legal-party, mapping, search and event contracts/fixtures.
- [x] Migration/domain/API/search/dedup tests and architecture evidence.

## Boundaries
Task 081 is the canonical party master. ERP, EDO/MChD, payments, procurement and compliance must reference these IDs rather than duplicate legal names/tax identifiers. Individual-entrepreneur PII remains subject to Task 060 privacy metadata and later Task 061 retention/subject-request workflows. Merge preview does not perform destructive merge; such writes require explicit workflow/Task-017 approval. Product compliance remains Task 082.

## Acceptance
INN/KPP/OGRN/OGRNIP data is canonical and not duplicated by downstream domains; merge/versioning is auditable; tenant/privacy tests pass. Required repository checks and architecture admission pass.
