# Task 039: CIAN vertical connector

Status: **repository-complete (2026-08-11)**.

## Objective
Audit current feed/partner API and implement Property publication/status integration through Vertical/Classified SDK.

## Dependencies
010, 017

## Deliverables
Implementation/spec/contracts/tests/docs required by the objective; update capability/event/API contracts when applicable.

## Acceptance
Property mapping, capability limitations and fixtures.

Run required repository checks and report results, risks and follow-ups.

## Completion evidence
- Registered provider `cian` under the classified Connector SDK boundary.
- Runtime capability is deliberately limited to `classified.publications.status.read`; CIAN XML publication is pull-based and is not misrepresented as an API write capability.
- API secret is callback-scoped Bearer material; health/status require exact configured feed URL and status additionally binds the exact remote import/order ID.
- Added deterministic Feed v2 mapping for bounded `flatSale` and `flatRent` apartment objects with category-specific `SaleType` / `LeaseTermType`, area/floor/price/phone/photo validation, unique `ExternalId`, object-count and output-size ceilings.
- Unsupported new-building/suburban/commercial schemas fail closed.
- Added import-state/report fixtures plus malformed/foreign-binding and XML-mapping tests.
- Task-064 provider conformance: 13/13 PASS; report SHA-256 `9b26c088d94dc80e777f25973817b937ad7e57b8c355436d78ac090b30291881`.
- Architecture gate: PASS (`75` modules, `14` providers, `51` reviews, `0` unreviewed).
- Root tests/vet/build, targeted race, 20x repeat, migrations, generated SDK drift, frontend and Linux sandbox checks pass under the local Go-1.23 compatibility run; canonical `go.mod` remains Go 1.26.0/toolchain 1.26.5.
- Canonical `make check` is environment-blocked before tests because this sandbox exposes Go 1.23.2 and cannot use the required Go 1.26 toolchain.

## Capability limitation / follow-up
The live CIAN ReDoc endpoint was discoverable during audit, but the isolated repository environment could not fetch the backing OpenAPI document. Provider code therefore uses typed `import.state` / `import.report` transport operations instead of inventing endpoint paths. A production HTTP adapter must bind those operations to the current official OpenAPI before deployment.

The canonical next dependency-ready task is `043 Instagram Connector`.
