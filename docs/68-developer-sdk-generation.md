# Generated Developer SDKs

Task 062 makes the public OpenAPI document the only source of generated REST client operations. The generator is intentionally independent of TORGNEXA Core and PostgreSQL packages: it reads `contracts/openapi/torgnexa-v1.yaml`, extracts the public operation surface, and emits supported Go, TypeScript and Python clients plus a hash-bound manifest.

## Supported outputs

- Go: `sdk/go/torgnexa/client.gen.go`, module `github.com/torgnexa/torgnexa-sdk-go`, minimum Go 1.23.
- TypeScript: `sdk/typescript/src/client.gen.mjs` plus generated TypeScript declarations `client.gen.d.mts`, ES2022/Fetch runtime.
- Python: `sdk/python/torgnexa_sdk/client_gen.py`, minimum Python 3.11, standard-library transport.

All three clients expose operation-specific methods generated from OpenAPI `operationId`, path/query parameters, JSON request bodies, bearer authentication, bounded responses and a common non-2xx error boundary. JSON/text representations are decoded normally, while binary media such as `application/pdf` is preserved byte-for-byte. They deliberately do not generate or expose SQL rows, repository structs, RLS fields, migrations or connector-internal types.

## Deterministic generation

Run:

```bash
make sdk-generate
make sdk-check
```

`make sdk-check` regenerates in memory and fails on drift, runs the generator tests, compiles/tests the Go SDK, syntax/tests the TypeScript runtime, type-checks declarations when `tsc` is present, compiles/tests Python, compares the OpenAPI SHA-256 with `contracts/sdk/generated-manifest.json`, checks operation inventory parity and rejects internal database-model tokens from all public SDK trees.

Generated files are committed. A change to the public OpenAPI cannot be merged as repository-complete when `sdk-check` reports drift.

## Base URL

The OpenAPI server path is `/api/v1`. SDK callers pass an absolute base URL including that path, for example `https://merchant.example/api/v1`. SDKs never accept organization/workspace selectors implicitly; tenant scope remains server-authenticated as defined by the API.

## Response typing boundary

Task 062 generates the transport/client operation surface, not a second hand-maintained public model hierarchy. JSON request bodies and response payloads remain `any`/`unknown`/`Any` at this stage and are governed by the linked public JSON Schema/OpenAPI contracts. This prevents accidental publication of internal Go/SQL models and avoids creating a second source of truth. Future additive generator improvements may emit public contract-derived models, but must continue to source them only from OpenAPI/JSON Schema.

## Versioning

SDK package version follows the public OpenAPI `info.version` for this repository snapshot. Additive `/api/v1` operations or optional fields may increment the API/SDK minor version. Breaking API behavior requires a new major API surface or the migration process defined in `contracts/sdk/compatibility-policy.md`; regeneration alone cannot legalize a breaking change.

The generator itself has an independent version (`torgnexa-sdkgen/v1`) recorded in every artifact and the manifest. A generator change that modifies emitted public signatures requires compatibility review even when the OpenAPI source hash is unchanged.
