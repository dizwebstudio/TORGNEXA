# Product Compliance

Task 082 makes product compliance a first-class, provider-neutral blocking domain. It is separate from marking/Chestny ZNAK and from connector implementations.

## Canonical evidence

`ComplianceDocument` records certificate/declaration/EAC/state-registration/veterinary/sanitary/refusal/information/other evidence with immutable document identity, jurisdiction, issuer/registry source, issue/expiry dates, holder party reference, optional evidence-object reference and registry verification metadata. Document status is versioned and terminal revocation/expiry cannot be silently reversed.

Evidence is attached through tenant-scoped bindings to a canonical Product, Offer, PIM Category or GTIN/SKU. PostgreSQL checks that product/offer/category subjects exist in the same tenant and validates GTIN checksums.

## Explainable policy engine

Policies are append-only versions. A policy can constrain jurisdiction, operation (`publication`, `sale`, `advertising`, `shipping`), connector family, seller role and category. Each requirement names a document type, required registry verification, minimum remaining validity and one failure outcome: `warn`, `approval_required`, or `block`.

Evaluation returns `allow`, `warn`, `approval_required` or `block`, plus machine reasons (`missing_evidence`, `expired_evidence`, `unverified_evidence`, `invalid_evidence`), the exact policy id/version and evidence ids considered. The result has a deterministic SHA-256 fingerprint.

## Publication guard

`products.write` in the Task-029 Connector Sandbox is fail-closed. A normal `NewSession` cannot execute `products.write`; host composition must use `NewGuardedSession` with an `OperationGuard`. `internal/platform/complianceguard` binds that host guard to the Task-082 evaluator. `block` denies the write; `approval_required` requires a host approval authorizer (Task 017); `allow` and `warn` may proceed.

The guard runs before provider executor code and before any mediated network egress, so connector code cannot choose to omit compliance evaluation.

## Registry verification

`RegistryVerifier` is a provider-neutral port. Verification updates the canonical document and appends immutable verification history; status/source/time are retained. A future official registry connector implements the port through the stable Connector SDK rather than entering Core.

## Expiry notifications

`ExpiryDue` returns bounded valid documents approaching expiry. `EmitExpiryAlerts` creates deterministic `expiry.<sha256>` alert ids and sends them through `ExpiryNotifier`. Task 022 Notification Center is the intended notifier implementation; deterministic ids allow idempotent delivery.

## Evidence and privacy

Every compliance repository mutation atomically commits Task-003 Audit, Task-008 Outbox and Task-030 Lineage evidence. Evidence payloads contain canonical ids/version/change only; certificate file contents, holder identifiers and registry response bodies are not copied into event/audit/lineage metadata.

## Operational rules

- No hard delete/truncate of compliance evidence/history through the application path.
- Policies and verification history are append-only.
- Forced RLS is mandatory on every compliance table.
- Expired/revoked/suspended/verification-failed evidence never satisfies a requirement.
- A requirement with `verification_required=true` cannot be satisfied by an unverified document.
- Product publication writes cannot execute without the host compliance guard.
