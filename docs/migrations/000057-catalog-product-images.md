# Catalog product images migration

Migration 000057 adds tenant-scoped product image metadata with stable IDs,
HTTPS URLs, bounded alt text, explicit ordering and optimistic versions.

Forced RLS binds every row to organization/workspace. Product identity and
creation metadata are immutable; updates must advance the version exactly once.
The table stores references and presentation metadata only, not uploaded image
bytes or remote credentials. Untrusted uploads must still pass the Task-088
quarantine and release policy before a downstream image reference is admitted.
