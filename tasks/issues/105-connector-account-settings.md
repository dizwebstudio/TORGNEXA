# Task 105: Connector Account Settings

## Status
`implemented`

## Objective
Create, list, update and disable tenant-scoped connector accounts through an authenticated API, PostgreSQL RLS, secret references and audit evidence.

## Dependencies
099, 100, 104, 005, 012, 021, 060, 092

## Acceptance
- bearer verification and tenant/workspace derivation are authoritative;
- credentials enter only a reviewed secrets provider and API responses expose references/status, never secret values;
- multiple accounts per provider are supported;
- writes are idempotent, versioned and audited;
- dangerous enable/disable changes pass policy checks.
