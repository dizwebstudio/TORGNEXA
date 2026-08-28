# Task 142: 5Post logistics connector

## Status

`repository-complete` — 2026-08-28. The separate Delivery surface now admits
tenant account setup, encrypted credentials and a live connection check;
shipment writes remain qualification-gated.

## Objective

Add 5Post as a provider-neutral logistics connector and expose the maximum
safe verification surface without advertising product synchronization or
unqualified shipment writes.

## Dependencies

074, 075, 090, 130

## Acceptance

- 5Post appears in the Delivery family of the integration catalog;
- its manifest claims only shipment create/cancel, tracking, labels and pickup
  points, all behind the Connector SDK v1 boundary;
- partner API-key/JWT material remains callback-scoped and no raw provider
  payload or recipient PII is persisted by the adapter;
- deterministic connector tests and the machine-readable conformance report
  pass without production credentials or network access;
- runtime support is `separate_surface/logistics`: account setup and
  credential checks work; shipment writes remain qualification-gated.

## Implementation evidence

- `connectors/fivepost` contains the typed host transport, normalized operations,
  deterministic candidate transport and tenant-scoped tests;
- the catalog manifest, presentation metadata, runtime-support contract and
  generated catalogs are synchronized;
- provider specification, capability audit, reconciliation notes and
  conformance plan/report live under `docs/connectors/fivepost`;
- architecture review `ARCH-142` records the provider boundary and the
  fail-closed runtime decision.

## Qualification

Repository tests validate the SDK boundary only. Live 5Post qualification is
blocked until a partner API key, current endpoint contract and host-side
logistics bridge are available; no such credentials belong in this repository.
