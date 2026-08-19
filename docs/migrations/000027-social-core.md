# Migration 000027 — Social Core

Phase: `expand`. Risk: `high`. Backup checkpoint required. Dependency: `000026`.

Creates the provider-neutral Social Core tables for content, immutable variants/media references, channel-account projections, publications and append-only publication-status events. All tables use organization/workspace composite tenancy and forced RLS.

The migration adds guards for content/publication lifecycle transitions, optimistic versions, social connector family/capabilities, currently released upload references, immutable schedules/snapshots/history and no hard delete/truncate. It stores no provider remote post ID, provider payload, credential, object key or signed URL.

No backfill is required and existing readers/writers remain compatible. Rollback is application rollback only; the additive schema and evidence are retained. A future contract migration may remove structures only after supported binaries no longer reference them and normal Task-067 contract gates pass.
