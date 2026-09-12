# ADR-0197 — OIDC subject surface minimization

Status: Accepted

## Context

The workspace-member API and Settings UI exposed the raw OIDC subject stored
on `workspace_members`. The same stable provider reference appeared in the
member privacy export. A subject is a pseudonymous personal identifier that can
correlate activity across requests and, depending on provider configuration,
across relying parties. Administrators need to know whether a member has
accepted an invitation, but do not need the identifier itself.

The reference is still required inside the trusted authentication path to
resolve active membership, bind a verified invitation and locate a user
profile. Privacy deletion also needs an opaque locator in order to clear every
matching record.

## Decision

Keep the OIDC subject in the tenant-scoped PostgreSQL membership record and in
internal repository/domain values. Remove it from the member response model,
OpenAPI schema and Settings UI. Every new member response includes
`identity_bound`, computed only as whether the internal reference is non-empty.
No prefix, issuer, length, hash or other correlatable derivative is exposed.

Member and profile privacy exports use the same boolean and omit the raw
reference. The privacy workflow may continue locating rows by an opaque OIDC
subject supplied through its trusted subject-request boundary. Restriction,
deletion and anonymization clear the binding. Correction changes profile and
member attributes only and cannot silently rebind identity.

Public contract validation rejects the exact internal field in every OpenAPI
document and event JSON Schema. API regression tests serialize bound and
unbound members and reject either the field name or a synthetic subject value.
The PostgreSQL integration profile checks actual list/update responses,
authoritative audit records, outbox payloads, privacy exports and post-deletion
storage.

## Compatibility and rollout

This is an additive minor REST contract release `0.22.0`. The previous subject
property was optional, so a conforming `0.21.x` client already had to accept its
absence. `identity_bound` is also optional in OpenAPI for the rolling-upgrade
window because an old API replica does not emit it. New API replicas always
emit an explicit boolean, and the UI treats a missing value from an old replica
as unbound. A future major contract may make the boolean required after old
replicas and SDKs are retired.

Generated clients in this repository return generic response bodies, so the
operation signatures do not change. Their API version and deterministic source
hashes advance with the OpenAPI source. Deploy API replicas before or together
with the UI; either order remains parse-compatible and never requires the UI to
render the old subject.

## Security, privacy and data lifecycle

The raw reference remains pseudonymous personal data protected by tenant RLS
and the existing membership lifecycle. It is absent from browser state,
ordinary management responses, SDK schema, audit summaries, events and export
artifacts. The boolean reveals only invitation/binding state already implied by
member status and cannot be used to address the identity provider.

No database migration or backfill is required. Existing bindings remain valid.
Deletion and restriction continue to set the reference to NULL, while the
member row remains as minimized authorization/privacy evidence under its
existing retention policy.

## Validation

Run unit/API tests, the contract checker, generated-SDK drift checks, frontend
logic/type/build validation, the forced-RLS PostgreSQL privacy/audit profile and
the complete repository gate. The PostgreSQL profile must cover bound and
unbound response projection, member mutation audit, event payload inventory,
export before deletion, deletion/anonymization effects and export after the
binding is cleared.
