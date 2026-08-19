# Task 003 — Audit Base

Production-grade tenant-scoped audit foundation with actor/source/action/resource/correlation/risk, bounded safe summaries, append-only persistence, PostgreSQL RLS, and defense-in-depth redaction.

## Status

Repository implementation completed on 2026-08-09.

The PostgreSQL runtime smoke is encoded in the repository and must run in CI or another environment with Docker/PostgreSQL available. This sandbox did not provide a Docker daemon or local PostgreSQL server.

## Acceptance

- [x] Canonical audit risk classes: `read`, `write_safe`, `write_sensitive`, `legally_significant`.
- [x] Required actor/source/action/resource/correlation/risk fields for every new application audit event.
- [x] Bounded `summary` JSON with recursive defensive copying and sanitization before persistence.
- [x] Redaction of Authorization/auth schemes, cookies, passwords, tokens, secrets, API keys, credentials, session identifiers, and private-key material.
- [x] Errors do not echo caller-provided audit summary data.
- [x] Append-only domain repository port; no update/delete method is exposed.
- [x] PostgreSQL adapter applies transaction-local tenant scope, revalidates sanitized records, and performs INSERT only.
- [x] Immutable migration `000004_audit_base.sql` adds risk/summary constraints without editing historical migrations.
- [x] Audit RLS is split into tenant-scoped SELECT and INSERT policies; no UPDATE/DELETE/ALL audit policy remains.
- [x] UPDATE, DELETE, and TRUNCATE are denied by database triggers, including attempts by a table owner/session that can otherwise bypass RLS.
- [x] PUBLIC mutation privileges are revoked as defense in depth.
- [x] Database-side checks reject common secret-bearing summary keys, raw auth-scheme values, and private-key PEM material.
- [x] JSON Schema contract and positive/negative contract fixtures cover the Task 003 event shape.
- [x] Unit/static tests cover sanitization, fail-closed validation, tenant mismatch, adapter SQL shape, migration RLS/append-only semantics, and atomic migration history.
- [x] PostgreSQL tenancy smoke script covers same-tenant read/insert, cross-tenant isolation, forbidden cross-tenant insert, and blocked UPDATE/DELETE/TRUNCATE.
- [x] Backup/restore and upgrade rehearsal scripts understand the first atomic migration (`000004`) and seed/apply migration history correctly.
- [x] Security/database/migration documentation updated.
- [x] Architecture freeze inventory updated with the PostgreSQL audit adapter and a fresh Task 003 `new_domain` review covering every changed sensitive Platform path.

## Validation evidence

Repository-local validation performed in this sandbox:

- Root `go test ./...`: PASS using the installed local Go compatibility toolchain after temporarily overriding only the local validation copy of the Go version; the repository `go.mod` was restored unchanged and still requires Go 1.26.0 / toolchain 1.26.5.
- Root `go vet ./...`: PASS under the same compatibility validation setup.
- `go run ./tools/migrationcheck --root .`: PASS; 4 migrations, latest `000004`.
- `bash -n` for the modified PostgreSQL tenancy, backup/restore, and upgrade scripts: PASS.
- Audit JSON Schema/fixture Draft 2020-12 validation: PASS using the available Python `jsonschema` implementation.
- Architecture repository validation: PASS (`modules=17`, `providers=0`, `reviews=2`).
- Synthetic merge-base-to-head architecture admission check against the original kit: PASS with 24 changed paths.
- Audit/migration packages `go test -race -count=1`: PASS; 20 repeated runs: PASS.

Environment limitations:

- Exact Go 1.26.5 validation could not run because the sandbox cannot reach the Go toolchain/module proxy.
- The isolated official contract checker could not download its uncached modules in this sandbox.
- PostgreSQL/Docker runtime smoke could not run because Docker/PostgreSQL are unavailable here.

These environment limitations do not change the repository implementation status; the exact-toolchain and PostgreSQL runtime checks remain mandatory CI qualification evidence.
