# Task 061 — Retention, subject requests and tenant deletion

The implementation lives in `internal/platform/retention` with durable PostgreSQL state in `internal/platform/postgres/retentionrepo` and migration `000039`.

Task 060 remains the policy registry. Task 061 does not redefine purposes, legal bases or retention periods; it executes the selected policy through explicit store adapters.

## Supported workflows

Subject requests support access/export, correction, deletion and restriction. A subject is represented by an opaque `{kind, id}` reference rather than email, phone, name or other raw PII. Correction bytes are not persisted in the workflow tables: only an upload/artifact reference is stored, and a production adapter must resolve/revalidate that reference through the Task-088 release gate immediately before use.

Retention expiry supports `delete`, `anonymize` and `archive_then_delete`. `manual_review` creates a blocked non-mutating workflow. Tenant deletion is a dedicated action and cannot start unless at least one authoritative store adapter is registered.

## Resumability

Each store has its own target row, cursor, processed count, version and evidence digest. `Advance` executes a bounded number of pages. On a store error, the target is left at its previous durable cursor. Retrying the same job therefore resumes at the last acknowledged page rather than starting over or skipping work.

## Legal holds

Tenant-, subject- and purpose/data-class-scoped holds are supported. Holds are immutable except for release and may expire. Subject deletion and tenant deletion evaluate holds before any destructive store call. Retention expiry follows the Task-060 `legal_hold_permitted` setting.

## Required production adapters

A deployment must register every store that can contain governed data. Typical targets are PostgreSQL authoritative tables, ClickHouse reporting projections from Task 049, search/cache projections, and S3-compatible object/evidence storage. A workflow is not complete until every configured target supporting the action reports completion.

The repository tests use authoritative, derived and object-store fakes to prove propagation and resume semantics. Live adapter/topology qualification remains deployment evidence because this repository sandbox does not contain production PostgreSQL/ClickHouse/object-storage services.
