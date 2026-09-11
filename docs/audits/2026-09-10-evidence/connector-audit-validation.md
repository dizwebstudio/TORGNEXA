# Connector audit validation — 2026-09-10

Base HEAD: `9524d8c5220a075626d31d04c83ff5a900070a73`; evidence covers the working
tree changes for ADR-0191 together with the earlier SSE changes. Go 1.26.7,
`GOTOOLCHAIN=local`, `GOTELEMETRY=off`, isolated build cache. No deployment.

OpenAPI SHA-256:
`0bc1d96667fcd352ab7c9702f2d0a2535c0b347607de8f70310183866ec9794c`.

| Command / scope | Result | Log |
| --- | --- | --- |
| Original credential rollback regression on PostgreSQL | Expected FAIL: binding escaped audit rollback | [before](connector-audit-before.log.txt) |
| `TORGNEXA_TEST_POSTGRES_IMAGE=torgnexa-postgres:18.6-hardened-amd64 ./scripts/check-audit-postgres.sh` | PASS: 33 top-level tests / 94 PASS including subtests | [PostgreSQL](connector-audit-postgres.log.txt) |
| `go test -race -count=1 -v ./internal/app/api ./internal/platform/secrets ./internal/platform/postgres/connectorrepo ./internal/platform/postgres/secretrepo ./internal/platform/postgres/syncrepo -run 'TestConnector\|TestBootstrap\|TestEnabledSchedule\|TestLocalEncryptedProvider\|TestScanPolicy'` | PASS: 17 unit tests; database cases skipped here and executed by the PostgreSQL gate above | [unit/race](connector-audit-unit.log.txt) |
| `go test ./...` | PASS: 215 packages with tests | [Go](connector-audit-go-test.log.txt) |
| `go vet ./...` | PASS, no output | [vet](connector-audit-vet.log.txt) |
| `./scripts/check-contracts.sh` | PASS | [contracts](connector-audit-contracts.log.txt) |
| `./scripts/check-architecture.sh` | PASS | [architecture](connector-audit-architecture.log.txt) |
| `./scripts/check-generated-sdks.sh` | PASS: 358 operations; global tsc unavailable | [SDK](connector-audit-sdk.log.txt) |
| `./frontend/node_modules/.bin/tsc -p sdk/typescript/tsconfig.json` | PASS using pinned repository compiler, no output | [SDK types](connector-audit-sdk-types.log.txt) |
| `./scripts/check-frontend-shell.sh` | PASS: tests, compilation, production build | [frontend](connector-audit-frontend.log.txt) |
| `make policy` | PASS, including module verification and image repository allowlist | [policy](connector-audit-policy.log.txt) |

Changed Go files were formatted with gofmt; `git diff --check` passed.
Provider responses use deterministic synthetic fixtures. PostgreSQL tests run
against a disposable network-disabled container using the full migration catalog,
forced RLS and a NOSUPERUSER/NOBYPASSRLS application role. The default pool has
one connection; the concurrent credential test uses four. Logs contain generated
synthetic tenant/account identifiers only, no live credentials or production PII.

The script waits for the final PostgreSQL TCP listener inside the isolated
container, avoiding the image's temporary Unix-only initialization server.
No production PostgreSQL, OAuth provider or running application stack was used.
