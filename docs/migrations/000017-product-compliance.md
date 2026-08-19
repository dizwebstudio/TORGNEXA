# Migration 000017 — Product Compliance

## Expand compatibility

Adds `compliance_documents`, `compliance_bindings`, append-only `compliance_policies`, and append-only `compliance_verifications`. No existing table or contract is dropped/renamed. Existing binaries ignore these tables.

## Integrity

Forced RLS scopes all records to organization/workspace. Binding guards require same-tenant Product/Offer/PIM Category references or a checksum-valid GTIN. Holder references must resolve to the same-tenant Legal Party/Counterparty core. PostgreSQL validates policy requirement JSON, document lifecycle/version progression and terminal evidence states.

## Rollback

Binary rollback leaves evidence tables intact. Do not remove product-compliance evidence as part of normal rollback. A future contract migration may remove structures only after explicit retention/legal review and compatibility gates.

## Verification

Run migration catalog checks, Task-082 repository audit, product-compliance unit tests, connector sandbox publication-guard tests, and PostgreSQL tenancy/restore rehearsals in CI/staging.
