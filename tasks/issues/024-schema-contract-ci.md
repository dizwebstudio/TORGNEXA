# Task 024

Add automated JSON/OpenAPI/proto contract checks and event naming/version policy checks to CI.

## Acceptance
Implementation + tests + docs/contracts; run required checks.

## Status

Completed on 2026-08-08.

- The isolated Go checker strictly compiles JSON Schema Draft 2020-12, OpenAPI 3.1 YAML, and proto3 without runtime network/filesystem reference resolution.
- Event names, catalog completeness/order, filename versions, gaps, envelope fields, schema IDs/titles, and valid/invalid fixtures are enforced.
- Resource/node bounds, symlink/path/reference restrictions, deterministic bounded sanitized diagnostics, cancellation, and important failure cases have automated tests.
- `make contracts` is the required local/CI entry point. Full check, build, race, repeated-test, fuzz, module-integrity, and independent review gates passed; evidence is recorded in `VALIDATION_REPORT.md`.
