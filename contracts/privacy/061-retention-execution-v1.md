# Privacy retention execution contract v1

Task 061 is the execution boundary for Task-060 privacy metadata.

## Workflow kinds

- `subject_request`: access/export, correction, deletion or restriction for one opaque subject reference.
- `retention_expiry`: executes an active Task-060 retention policy.
- `tenant_deletion`: deletes/anonymizes tenant-owned data through every configured target.

## Durable invariants

1. Organization/workspace scope is mandatory on every repository and store call.
2. A target cursor advances only after that exact page succeeds.
3. Store failures do not advance cursors or processed counters.
4. Evidence is append-only and contains no raw subject payload.
5. Destructive workflows fail closed while a matching legal hold is active.
6. `manual_review` never performs automatic mutation.
7. Destructive workflows require at least one authoritative target.
8. Export and archive-then-delete completion require an artifact reference.
9. Derived stores are explicit targets; a job is complete only when every target is complete.
10. Corrections carry only an opaque released-artifact reference; the executing adapter must revalidate it through Task 088 before reading bytes.

## Store step

A store step receives: workflow ID, action, opaque subject reference or retention purpose/class, correction artifact reference, previous cursor and a bounded page limit. It returns the next cursor, affected count, SHA-256 evidence digest, optional artifact reference and completion flag.

For non-final pages `next_cursor` must be non-empty and different from the previous cursor. Final pages must clear `next_cursor`.
