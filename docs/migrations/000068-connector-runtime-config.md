# Migration 000068 — connector runtime config

Adds `connector_runtime_configs`, a tenant-scoped store for **non-secret** provider configuration required by production connector bridges (for example Yandex Market campaign/business IDs, 1C host/resource mappings and WooCommerce store host/currency).

## Security invariants

- Credentials, tokens, passwords, API keys, private keys and authorization material remain in `SecretProvider` and are rejected from this table.
- `ENABLE ROW LEVEL SECURITY` and `FORCE ROW LEVEL SECURITY` are both enabled.
- Select/insert/update policies require the current organization/workspace scope.
- Delete/truncate are denied; connector account lifecycle is represented by account state rather than destructive configuration deletion.
- The API uses optimistic `version` writes and records only connector/config-version metadata in audit evidence; configuration payloads are not copied into audit summaries.

## Compatibility

Expand-only after migration 000067. Old binaries ignore the table. New API/worker binaries require this schema for versioned runtime configuration used by configuration-bearing connector bridges.

## Rolling compatibility

The migration is additive. Old readers/writers ignore the table. New binaries can start on the previous schema: configuration-dependent connector operations remain unavailable until `000068` is applied, while existing operations continue to function. No startup path requires this table to exist before migration deployment.
