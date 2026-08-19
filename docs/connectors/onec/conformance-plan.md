# 1C conformance plan

Task 015 uses the shared Task-064 13-check harness with the real Task-029 Linux namespace/chroot sandbox fixture.

Canonical report: `docs/connectors/onec/conformance-report.json`.

Provider-specific deterministic tests additionally cover:

- configured Unicode OData resources/fields;
- metadata health boundary;
- Basic credential parsing/redaction boundary;
- ordered `$top/$skip` pagination;
- cursor/config fingerprint binding;
- catalog archive/revision mapping;
- exact decimal inventory;
- duplicate/malformed/exponent quantity rejection and exact signed balance preservation;
- bounded normalized remote errors.

Production enablement additionally requires a least-privilege read-only 1C user, TLS publication, live `$metadata` smoke test and validation of the configured catalog/register field names against that infobase.
