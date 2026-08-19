# Migration 000004 — audit base

Task 003 upgrades `audit_records` from the bootstrap placeholder to the application audit boundary.

## Expand compatibility

`risk` is added with the temporary-compatible value `unclassified`. This preserves writers that predate Task 003 and do not send a risk column. New application code never emits `unclassified`; it must use `read`, `write_safe`, `write_sensitive`, or `legally_significant`.

The migration keeps the existing nullable shape of legacy `actor_id` and `correlation_id` rows so the expand phase does not invalidate pre-Task-003 data. The production `audit.Service` requires both fields for every new record.

## Append-only enforcement

Application access is split into exactly two forced-RLS policies:

- `SELECT` for the active `app.organization_id` + `app.workspace_id` scope;
- `INSERT` with the same tenant check.

There are no application `UPDATE`, `DELETE`, or `ALL` policies. In addition, database triggers reject `UPDATE`, `DELETE`, and `TRUNCATE` even for a privileged session unless schema maintenance explicitly disables/replaces the guards. The application repository itself exposes only `Append`.

Retention/legal-hold work in later tasks must use an explicit privileged maintenance design; it must not add normal application mutation methods to the audit repository.

## Redaction and bounds

The Go audit service deep-copies and sanitizes summaries before persistence. Secret-like keys, raw HTTP authorization schemes, and private-key material are replaced with `[REDACTED]`. Unsupported arbitrary Go values, excessive nesting/node counts, and summaries above the bounded size are rejected.

The PostgreSQL schema adds defense-in-depth checks for JSON-object shape, size, common credential-bearing key names, raw Authorization-style values, and private-key PEM markers. These checks complement the service/repository validation; they do not replace it.

## Atomic history

This is the first post-framework migration and therefore uses `history_mode: atomic`. The migration controller must set the reviewed `torgnexa.migration_*` session values before execution; the migration writes its own `migration_history` row in the same transaction.
