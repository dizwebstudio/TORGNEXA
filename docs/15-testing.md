# Testing Strategy

Required layers:

- unit/domain invariant tests;
- API/event/schema contract tests;
- PostgreSQL migration tests and interrupted/resumed backfill tests;
- connector deterministic fixtures + conformance suite;
- outbox/inbox/idempotency/retry/DLQ tests;
- sync/reconciliation drift scenarios;
- privacy/redaction/tenant-isolation tests;
- approval/risk/AI prompt-injection guard tests;
- signed webhook/replay/SSRF tests;
- load/SLO/failure-injection profiles for critical paths;
- upgrade rehearsal and backup/restore tests for releases.

The PostgreSQL restore gate is `make backup-restore-runtime`. It performs both
logical restore and physical base-backup/WAL PITR, checks tenant RLS after each
restore, and proves corrupted backup detection. It uses synthetic data only;
production storage/KMS qualification still requires the isolated operational
drill defined in `docs/runbooks/postgresql-backup-restore.md`.

The migration policy gate is `make migrations`; the runtime rehearsal is
`make upgrade-runtime`. Tests cover checksum/inventory drift, history gaps and
newer-database downgrade denial, unsafe/destructive SQL, phase metadata,
processor error/panic redaction, invalid cursors/bounds, interruption before
checkpoint commit, idempotent retry, lease fencing/reclaim, old-shape rolling
compatibility, and upgraded/fresh schema parity.

External APIs must be mocked/fixture-driven in CI unless a separately configured sandbox test is explicitly enabled. Never use live production credentials in CI.

## Contract gates

`make contracts` and `./scripts/check-contracts.sh` are the canonical local and CI entry points. They run the isolated `tools/contractcheck` module with a read-only module graph, including its unit/fuzz seed tests and vet, then validate the repository contracts. The gate fails closed; missing optional Python packages never disable YAML or OpenAPI validation.

Contract tests cover both a minimal accepted document and a targeted rejected document for every JSON Schema. Fixtures live in `contracts/fixtures/schema-fixtures.json`, contain synthetic identifiers only, and are sorted by schema path. Invalid fixtures should mutate one relevant constraint such as `required`, `type`, `format`, `pattern`, `enum`, `uniqueItems`, or `additionalProperties`; an invalid fixture that becomes accepted fails CI.

The checker also has deterministic negative tests for duplicate JSON/YAML keys, unsafe references, malformed schemas, OpenAPI versions and operation IDs, protobuf imports and syntax, event catalog ordering/version gaps, unsafe paths, file limits, cancellation, and sanitized diagnostics.

## Architecture gate

`make architecture` validates the current module/provider inventory, structured
impact records, referenced tasks/ADRs/evidence, source layout, and Go dependency
direction. It inventories the complete repository and accepts Go only beneath
explicit runtime/provider/tool roots. It parses sources regardless of test or
build tags and rejects unsafe paths, symlinks, duplicate/unknown JSON fields,
placeholders, every root or nested `vendor` tree, unregistered packages,
provider-specific non-provider code,
unapproved first-party roots, cgo/linkname escapes, and direct provider access
to Core/App/database internals. Every checked-in architecture review is also
validated against the governance JSON Schema by the contract gate.

Pull-request CI first builds the checker from the exact base SHA with network
access disabled and runs it against the exact HEAD before executing HEAD code.
Tests cover add/modify/delete/rename/copy parsing, class-to-scope and provider-ID
binding, missing/stale records/evidence/ADRs, new-domain and mixed rules,
merge-base admission prerequisites, incomplete impact matrices, malformed and
bounded inputs, cancellation, deterministic sanitized diagnostics, and
shallow/sparse/dirty/mismatched/ignored-untracked Git state.

This repository workflow is not by itself a hosted trust anchor because a pull
request can edit its workflow definition. Operational qualification therefore
requires a protected Ruleset Required Workflow (or equivalent immutable
external check) and a retained post-merge pull-request run proving that the
base-built verifier and required architecture reviewer could not be skipped.
