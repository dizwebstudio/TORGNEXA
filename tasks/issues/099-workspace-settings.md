# Task 099: Workspace Settings

## Status
`implemented`

## Objective
Add tenant-scoped organization/workspace profile settings with PostgreSQL persistence, optimistic concurrency and append-only audit evidence.

## Dependencies
098, 003, 021, 060

## Acceptance
- contract-first read/update API under `/api/v1/settings/workspace`;
- tenant context comes only from authenticated claims;
- mutations require idempotency and version preconditions;
- changes are audited and covered by PostgreSQL RLS tests;
- Settings UI supports validation, conflict and retry states.
