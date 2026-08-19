# ADR 0045: Product compliance registry and fail-closed publication enforcement

Status: Accepted

## Context
Marketplaces, ERP flows and regulated product channels require certificates, declarations, EAC and other evidence. If each connector implements these rules independently, evidence/status drifts and a connector can publish a listing despite missing or expired documents.

## Decision
Task 082 adds canonical `internal/core/compliance`, PostgreSQL `compliancerepo`, and host `complianceguard`. Evidence is scoped to Product/Offer/PIM Category/GTIN and legal-party holders. Policies are append-only versions with jurisdiction/operation/category/seller/connector-family matching and explainable `allow|warn|approval_required|block` outcomes.

Task-029 Connector Sandbox becomes fail-closed for `products.write`: a host `OperationGuard` is required before provider executor code runs. `complianceguard` evaluates Task-082 policy; `approval_required` may proceed only through a Task-017 approval authorizer. Registry verification and expiry notification are provider-neutral ports.

## Consequences
Future marketplace/ERP connectors cannot bypass Product Compliance by omitting an SDK call. Product publication workflows must provide canonical evaluation context. Official registry verification and Notification Center remain replaceable adapters. Compliance decisions can be audited by policy version and evidence ids.

## Alternatives considered
Connector-local compliance checks were rejected because they create provider-specific policy drift and bypass risk. Embedding certificates directly in Product was rejected because evidence has independent lifecycle/verification/scope. Automatically treating uploaded documents as valid was rejected because authoritative verification may be required.

## Compatibility impact
The change is additive. Existing compliance v1 schemas/events remain published unchanged; Task 082 adds v2/typed schemas and a new additive record-changed event. Connector SDK root interfaces do not change; Task-029 Session gains an additive guarded constructor and fail-closed behavior only for `products.write`.

## Migration and data impact
Expand migration `000017_product_compliance.sql` creates new tenant-scoped evidence/policy/history tables. No old column is renamed or dropped. Policy versions and verification history are append-only.

## Security and privacy impact
Forced RLS and same-tenant subject/holder guards prevent cross-tenant evidence references. The publication guard executes before provider code/egress. Audit/outbox/lineage metadata excludes document bodies and sensitive holder/registry payloads. Evidence-object security remains Task 088.

## Operational impact
Expiry scans are bounded and deterministic alerts can be delivered idempotently by Task 022. Registry verifier failures are explicit. CI/staging must qualify PostgreSQL RLS/triggers and the connector publication no-bypass test before product-write connectors are admitted.
