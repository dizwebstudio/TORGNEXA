# Migration 000063 — connector sync claim fix

This forward-only expand migration resolves a PL/pgSQL name-resolution ambiguity in the Task-108 `RETURNS TABLE` scheduler claim function. It sets `plpgsql.variable_conflict=use_column` for that single function, so qualified queue columns take precedence over output-variable names in the internal `INSERT ... RETURNING` statement.

No table, row or tenant policy changes. Old readers and writers remain compatible. The fix must be applied before starting the Task-108 scheduler; rollback is operational by stopping that process, while the harmless function setting remains in place.

Checksum: `80d26f5507e5606cad55a9d922acc8aa7e4e96deed67592a57ae2e5ad57399c4`.
