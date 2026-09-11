# Privileged audit completion validation — 2026-09-11

Base HEAD: `9524d8c5220a075626d31d04c83ff5a900070a73`; evidence covers the
working-tree implementation of ADR-0193 together with earlier uncommitted Task
234 fixes. Go 1.26.7, `GOTOOLCHAIN=local`, isolated build caches. No deployment.

OpenAPI SHA-256:
`fe76c9c9e2ed0451ffde9ffd1878112d2c6cf822d64a9590d0606461859cbf1b`.

| Command / scope | Result | Log |
| --- | --- | --- |
| `./scripts/check-audit-postgres.sh` | PASS: 39 top-level / 100 including subtests, `-race` | [PostgreSQL](privileged-audit-postgres.log.txt) |
| `go test ./...` | PASS: 215 packages with tests | [Go](privileged-audit-go-test.log.txt) |
| `go vet ./...` | PASS, no output | [vet](privileged-audit-vet.log.txt) |
| `./scripts/check-contracts.sh` | PASS | [contracts](privileged-audit-contracts.log.txt) |
| `./scripts/check-architecture.sh` | PASS: 168 modules / 61 providers / 220 reviews | [architecture](privileged-audit-architecture.log.txt) |
| `./scripts/check-migrations.sh` | PASS: 62 migrations, latest 000062 | [migrations](privileged-audit-migrations.log.txt) |
| `./scripts/check-generated-sdks.sh` | PASS: 358 operations | [SDK](privileged-audit-sdk.log.txt) |
| `frontend/node_modules/.bin/tsc -p sdk/typescript/tsconfig.json` | PASS | [SDK types](privileged-audit-sdk-types.log.txt) |

All changed Go files were formatted with `gofmt`; `git diff --check` passed.
Fixtures contain generated tenant/account identifiers and synthetic credential
material only. The PostgreSQL container runs without a network, and no live
provider, production database or running Community stack was used.
