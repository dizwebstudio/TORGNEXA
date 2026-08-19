# Migration 000016: Legal party / counterparty core

Expand-only migration creating canonical legal entities, individual entrepreneurs, branches, counterparties, addresses, bank accounts, contracts, authority references, duplicate candidates and immutable merge previews. It also additively broadens generic connector entity mappings to legal-party entity types.

All new tables are tenant scoped and use forced RLS. Canonical/history rows cannot be hard-deleted or truncated. Existing mapping v1-v3 contracts are not changed; mapping v4 publishes the broadened type set.
