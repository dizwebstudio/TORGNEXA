# Task 104: Integration Catalog Settings

## Status
`repository-complete` — 2026-08-15.

## Objective
Expose the canonical connector catalog in Settings from a deterministic projection of reviewed connector manifests, without provider-specific frontend branches.

## Dependencies
098, 010, 064

## Acceptance
- generated catalog is derived only from validated `connectors/*/manifest.json` metadata;
- marketplace and other connector families are grouped generically;
- no credentials, secret values, tenant identifiers or remote account data enter the projection;
- catalog drift fails repository validation;
- account creation is not presented as active until Task 105 supplies an authenticated backend workflow.
