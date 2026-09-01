# Legal Entity / Counterparty Core (Task 081)

Task 081 establishes the canonical enterprise party master used by ERP, EDO/MChD, payments, procurement and compliance.

## Model

- `LegalEntity`: stable legal organization identity; country-neutral master with typed RU INN/KPP/OGRN validation when `country_code=RU`.
- `IndividualEntrepreneur`: natural-person business master; RU INN/OGRNIP validation and privacy-sensitive full name.
- `Branch`: child of one LegalEntity; RU KPP validation when applicable.
- `Counterparty`: a tenant role (`customer`, `supplier`, `partner`, `other`) referencing exactly one party master.
- `Address`, `BankAccount`, `Contract`, and `AuthorityReference`: versioned subordinate records used by enterprise integrations.

Remote ERP/EDO/provider identifiers stay in `connector_entity_mappings` v4. They never become fields on canonical party structs.

## Search and deduplication

`GET /api/v1/counterparties/search` is tenant-scoped from authenticated context and supports bounded name/INN/registration-id search. Explainable duplicate detection gives exact identifier and normalized-name signals. Merge preview is deterministic, fingerprinted, tenant-bound, and non-executing; actual destructive merge remains a later approved workflow.

The UI and public API now expose the complete initial workflow. `POST
/api/v1/legal-parties` creates a draft canonical master (legal entity, IP or
branch), while `POST /api/v1/counterparties` assigns a role to an existing
master. The role endpoint verifies the referenced party in the same
organization/workspace before writing the relationship, so counterparty roles
cannot become detached or duplicate legal-party реквизиты.

## Evidence

Every repository mutation commits the canonical row together with Task-003 Audit, Task-008 Outbox event `enterprise.legal_party.record_changed.v1`, and Task-030 Lineage evidence in one transaction. Audit/event payloads contain IDs/version/change only; names, INN, bank account numbers and authority details are not copied into those logs.

## Privacy and security

IndividualEntrepreneur names and identifiers can be personal data. All tables use forced RLS and same-tenant references. Search uses authenticated tenant scope and `no-store`; hard delete/truncate is denied in normal application paths. Retention/subject-request execution remains Task 061.
