# Wildberries conformance plan

Task 011 is the first admitted marketplace provider after Tasks 010, 025, 029 and 064 are already repository-complete.

## Required machine evidence

Canonical report: `docs/connectors/wildberries/conformance-report.json`.

The provider adapter runs all thirteen Task-064 checks. Its sandbox portion uses the provider-neutral `conformance.SandboxFixture`, while WB-specific probes remain in `connectors/marketplaces/wildberries` and the functional API tests use deterministic fixtures. The machine report must match manifest ID/version/SDK major and pass every required check.

## Semantic fixture coverage

- manifest JSON equals executable manifest and declares the bounded
  `products.write` slice in addition to the read capabilities;
- `/ping` health covers both required API domains;
- rejected auth is bounded and normalized;
- product card response maps `nmID`, `chrtID`, SKUs and UTC update time;
- product cursor round-trip is deterministic and malformed cursors fail closed;
- seller warehouses are bounded and validated;
- stock requests send `chrtIds` and reject unexpected/duplicate IDs or negative quantities;
- 429 retry metadata is bounded and raw remote body is never surfaced;
- transport failure does not propagate a raw error string;
- product snapshot create/update uses the documented Content API path, forwards
  idempotency metadata, never sends an arbitrary media URL and keeps accepted
  distinct from published;
- provider package imports only the Connector SDK prefix and approved standard library.

## Sandbox / network qualification

Task-064 conformance verifies production-secret rejection, dry-run side-effect suppression, exact egress grants, output limits and Linux namespace/chroot isolation. Production transport grants for this provider are limited to the two official WB API DNS names on TLS/443. Fixtures never use production credentials or live seller data.

## Live qualification

Repository completion is deterministic/offline. Before production enablement for a seller account, run a smoke qualification with a dedicated least-privilege test/sandbox credential: both `/ping` checks, one bounded card page, warehouse list and a bounded stock read. Do not persist returned business payloads as conformance evidence.
