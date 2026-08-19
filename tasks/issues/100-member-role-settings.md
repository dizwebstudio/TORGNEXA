# Task 100: Member and Role Settings

## Status
`implemented`

## Objective
Provide default-deny workspace membership and role administration without treating external identity-provider groups as trusted application roles.

## Dependencies
099, 017, 060, 084

## Acceptance
- list/invite/disable members through reviewed tenant mappings;
- role changes require explicit authorization and append-only audit;
- the last active workspace administrator cannot remove their own recovery path;
- pagination is cursor-based and all writes are idempotent;
- UI exposes only actions allowed by authoritative capabilities.

## Implementation

- `/api/v1/settings/members` provides cursor-paginated listing and idempotent invitation mappings;
- `/api/v1/settings/members/{member_id}` changes role/status with optimistic concurrency and append-only audit;
- PostgreSQL forced RLS isolates workspace membership and protects immutable identity fields;
- the final active administrator cannot be disabled or demoted;
- Settings renders invitation, role assignment and blocking controls only for `settings.members.write`.
