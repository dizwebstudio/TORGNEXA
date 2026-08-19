# Schema Registry & Contract Governance

Repository contracts are the source of truth for public REST, event, webhook, plugin, and protobuf interfaces. Runtime modules must not depend on a schema-registry vendor. Introduce an external registry only when operational governance requires it and preserve the repository artifacts as the reproducible baseline.

## Required CI gate

Run either `make contracts` or `./scripts/check-contracts.sh`. CI runs the same command through `make check`. The checker is an isolated Go module at `tools/contractcheck`; its versions and checksums are pinned, its module graph is read-only during validation, and all contract references are resolved from memory. The validator never fetches a schema or OpenAPI reference from the network or arbitrary filesystem paths.

The gate enforces:

- strict UTF-8 JSON and YAML, with duplicate keys, trailing JSON, aliases, anchors, merge keys, custom YAML tags, multiple YAML documents, symlinks, and non-regular files rejected;
- a maximum of 4 MiB per file, 10,000 files, 64 MiB total, 128 nesting levels, 100,000 JSON/YAML nodes per document, 1,000,000 aggregate nodes per format, a cooperative 30-second CLI cancellation deadline, and bounded sanitized diagnostics;
- JSON Schema Draft 2020-12 meta-schema compilation with format assertions;
- OpenAPI exactly 3.1.0, YAML source only, internal `$ref` fragments only, `$dynamicRef` rejected, semantic validation, and deterministic unique non-empty `operationId` checks;
- linked proto3 compilation from an immutable in-memory source map, with only repository protobuf files and standard Google protobuf imports available;
- complete schema fixtures and the event naming/version rules below.

## JSON Schema identity and references

Every `*.schema.json` has a non-empty `title`, a unique exact `$id`, and exact `$schema` value `https://json-schema.org/draft/2020-12/schema`. New schema IDs are derived from the contract-relative path:

```text
contracts/fx/rate.schema.json
→ https://torgnexa.local/schemas/fx/rate.json
```

The already-published event envelope keeps its compatibility ID `https://torgnexa.local/schemas/event-envelope.json`; do not rename it. Nested `$id` values are forbidden. `$ref` and `$dynamicRef` may target an internal fragment or another preloaded repository schema by its exact ID/relative resolution. Network, `file:`, traversal, and unresolved targets fail validation.

Every schema is registered exactly once in the sorted `contracts/fixtures/schema-fixtures.json` catalog with one synthetic valid fixture and one targeted invalid fixture. Production PII, credentials, signing material, tokens, and real external identifiers are forbidden in fixtures.

## Event policy

`contracts/events/event-catalog.json` is the complete, strictly sorted mapping of event types to payload schemas. Each payload schema appears exactly once and its `title` equals the event type.

Event types use exactly `<domain>.<entity>.<action>.vN`, with lowercase alphanumeric snake-case segments. Versions start at `v1`, are limited to `v999`, match the `-vN.schema.json` filename, and have no gaps within a family. The canonical envelope requires `event_id`, `event_type`, `occurred_at`, `organization_id`, `workspace_id`, `correlation_id`, `causation_id`, `entity_type`, `entity_id`, `source`, and `data`.

Breaking payload semantics require a new `.vN+1` event type. Public REST changes inside `/api/v1` must remain additive. The current gate proves the current artifact set is internally valid and versioned; compatibility against a merge-base snapshot still requires explicit review until a deterministic cross-revision diff gate is added.

## Change procedure

1. Update the contract before or with implementation code.
2. Preserve published IDs and add a new event version for breaking semantics.
3. Update the event catalog when an event version is added.
4. Add/update synthetic valid and targeted invalid fixtures.
5. Run `make contracts`, then the repository-required `make check` and build gates.
6. Record compatibility, security, privacy, and migration impact in the task/ADR when applicable.

Generated clients and artifacts are derived from committed contracts, versioned, and reproducible; they are never generated from internal Go structs.
