# Task 107: Connector Capability Settings

## Status
`completed`

## Objective
Configure enabled connector capabilities and synchronization directions within manifest-declared limits.

## Dependencies
105, 106, 013

## Acceptance
- only manifest-declared capabilities can be enabled;
- read/write directions are explicit and default deny;
- write-sensitive capabilities require policy/approval classification;
- changes are versioned and audited;
- Core remains provider-neutral.

## Implementation

- `PUT /api/v1/connector-accounts:capabilities` replaces the exact enabled subset with optimistic account versioning and immutable audit evidence.
- Migration `000060` stores complete append-only, tenant-scoped snapshots under forced RLS; an account without a snapshot is denied every capability.
- Direction, risk and approval requirements are host-owned metadata. Connector manifests and API callers cannot downgrade a remote write from `write_sensitive` or remove its approval requirement.
- Account activation and sync-policy creation, re-enabling and manual execution fail closed unless the required per-account capabilities are enabled.
- Settings → Integrations renders every manifest capability with explicit read/write classification and saves the cabinet-specific selection.
- Architecture validation registers the existing catalog-image/report adapters, recognizes the Connector SDK `storefront` family and excludes the registered frontend and n8n package-manager dependency roots from first-party Go inventory; hidden source-side vendor and symlink paths remain rejected.
