# Task 084: Enterprise Iam Federation

## Status
`repository-complete` — 2026-08-12.

## Implementation evidence
- explicit federation mappings, default deny, offboarding revocation/audit and migration 000051.
- Architecture review `ARCH-084` and executable tests are included.

## Objective
Implement Enterprise IAM federation/provisioning baseline: LDAP/AD, SAML, SCIM, JIT, service accounts and reviewed group/claim-to-role mapping on top of Keycloak/OIDC.

## Dependencies
002, 003, 021, 060

## Deliverables
IAM mapping model/config, adapters/configuration guidance, offboarding/session/key revocation, reconciliation, security/audit tests and docs.

## Acceptance
Unmapped identities receive no implicit roles; deprovisioning revokes access; tenant mapping is explicit; privileged IAM changes emit security audit.

Run required repository checks and report results, risks and follow-ups.