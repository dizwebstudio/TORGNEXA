# Webhook A03–A04 validation

Date: 2026-09-09. PostgreSQL 18.6, Go 1.26.7, disposable database,
network disabled; synthetic fixtures; no production/provider writes.

- `./scripts/check-audit-postgres.sh`: PASS with `-count=1 -race`;
  18 top-level tests / 40 including subcases for A03–A08.
- `go test ./...`: PASS, 215 packages with tests.
- `go vet ./...`: PASS, empty log.
- `./scripts/check-contracts.sh`: PASS.
- `./scripts/check-architecture.sh`: PASS.
- `./scripts/check-generated-sdks.sh`: PASS, 358 operations.
- `node frontend/node_modules/typescript/bin/tsc -p sdk/typescript/tsconfig.json`:
  PASS (the SDK script had no global tsc).
- `gofmt` and `git diff --check`: PASS. SQL migrations unchanged.

For the database check, `TORGNEXA_TEST_POSTGRES_IMAGE` was set to
`torgnexa-postgres:18.6-hardened-amd64`; the script applies all 62 migrations
and removes its own temporary container on exit. The ordinary Go test command
skips database scenarios unless both explicit test database URLs are supplied.
The dedicated script supplies them for an isolated database and application role
without SUPERUSER/BYPASSRLS. Injected errors in the log are expected assertions.

Logs (SHA-256):

- [webhook-postgres.log](webhook-postgres.log): `a549ea9051a1113e9f0562264c17448dc67db3eb6013d4813b17f14460b8528b`
- [webhook-go-test.log](webhook-go-test.log): `773140052cf00227b7d6b691c71d5097576d3f83e94979c4d1b5d3a73647f3f3`
- [webhook-vet.log](webhook-vet.log): `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`
- [webhook-contracts.log](webhook-contracts.log): `7d2428a7a97bee992a673e3aa2b1b46b81d86cc478e2d5104b7094df8cd2fe91`
- [webhook-architecture.log](webhook-architecture.log): `393f64814be7ff3f9cd6c5595a29c4e060c3efd8d4e3c4f2830d03b2f098530d`
- [webhook-sdks.log](webhook-sdks.log): `8dee3b80c519872e2d4e9668e353d14fd1074e14d6c06f3629762a2a2e2c4796`
- [webhook-typescript.log](webhook-typescript.log): `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`
