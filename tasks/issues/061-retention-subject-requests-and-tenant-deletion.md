# Task 061: Retention, subject requests and tenant deletion

## Status
`repository-complete` — 2026-08-12.

## Objective
Implement coordinated export/correction/deletion/anonymization/legal-hold workflows across stores.

## Dependencies
060, 026, 049

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Resumable jobs, evidence/audit, derived-store propagation tests.

Run required repository checks and report results, risks and follow-ups.

## Implementation evidence
- `internal/platform/retention` coordinates subject access/export, correction, deletion and restriction; retention expiry; tenant deletion; and tenant/subject/purpose-class legal holds.
- each authoritative/derived/object store is an explicit resumable target with its own versioned cursor, processed counter and checksum evidence;
- failed target calls do not advance durable cursors, and retries resume from the last acknowledged page;
- destructive workflows require an authoritative target and evaluate active legal holds before any store mutation;
- Task-060 `manual_review` is fail-closed as a blocked non-mutating workflow;
- export and archive-then-delete require a final artifact reference before a target may complete;
- the coordinator persists only opaque subject identifiers and artifact references, never correction/export payload bytes;
- correction adapters must revalidate the referenced upload through Task 088 before consuming bytes;
- lifecycle events use the existing append-only audit service with `legally_significant` risk;
- `internal/platform/postgres/retentionrepo` persists workflow state under forced tenant RLS;
- migration `000039_retention_subject_requests_tenant_deletion.sql` adds subject request, legal hold, job, target and append-only evidence tables; legal holds are immutable except for explicit release;
- ADR `0063`, architecture review `ARCH-061`, execution contract and operator documentation are included.

## Qualification
- Task-061 unit tests: PASS, including authoritative + ClickHouse-like derived + object-store propagation, failed-page resume, legal-hold block/release, retention hold policy, manual-review fail-closed behavior and authoritative-target requirement;
- architecture: PASS — `88` modules, `19` providers, `69` reviews, `0` unreviewed changes;
- migrations: PASS — `39/39`, latest `000039`;
- no public OpenAPI, existing event schema or Connector SDK compatibility change.

Deployment follow-up: production must register the real topology's PostgreSQL, ClickHouse, search/cache and object-storage Task-061 adapters and re-run destructive-workflow qualification against those services before enabling tenant deletion.
