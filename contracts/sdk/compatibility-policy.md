# Compatibility Policy

- Public REST API: additive changes within `/api/v1`; breaking changes require a new major API surface or documented migration window.
- Events: breaking payload semantics require `.vN+1` event type.
- Connector SDK: semantic versioning; plugins declare compatible core + SDK ranges.
- Webhooks: same event versioning as Kafka-facing public event contracts where appropriate.
- Generated SDKs are versioned from OpenAPI and never generated from internal Go structs.

## Generated REST SDKs

- `contracts/openapi/torgnexa-v1.yaml` is the sole operation source for the supported Go, TypeScript and Python REST SDKs.
- Generated package version follows OpenAPI `info.version` for the repository snapshot; public package publication remains subject to Task 065 release/license gates.
- Additive `/api/v1` operations and optional parameters may ship in a minor SDK release. Removing/renaming an operation, required parameter, or changing request/response semantics incompatibly requires a new major API surface or an approved migration window.
- The generator version is independent from the API version. A generator change that alters emitted public signatures requires compatibility review even when the OpenAPI hash does not change.
- Generated SDKs may reference public OpenAPI/JSON Schema concepts only. Imports/references to `internal/*`, PostgreSQL rows, migrations, sqlc/ORM/driver models are forbidden.
- Committed artifacts must exactly match deterministic regeneration and the SHA-256/source operation inventory recorded in `generated-manifest.json`.
