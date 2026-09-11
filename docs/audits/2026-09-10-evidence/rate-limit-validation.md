# Task 234.8 validation evidence

Date: 2026-09-10

| Check | Result | Evidence |
|---|---|---|
| RED multi-replica regression before fix | FAIL as expected: aggregate third request returned `204` | `rate-limit-before.log.txt` |
| Real Valkey, two limiter instances, two API handlers, cardinality and concurrent race tests | PASS | `rate-limit-valkey.log.txt` |
| Local limiter and API rate-limit race tests | PASS | `rate-limit-race.log.txt` |
| `go test ./...` | PASS | `rate-limit-go-test.log.txt` |
| `go vet ./...` | PASS | `rate-limit-vet.log.txt` |
| `./scripts/check-contracts.sh` | PASS | `rate-limit-contracts.log.txt` |
| `./scripts/check-architecture.sh` | PASS | `rate-limit-architecture.log.txt` |
| `./scripts/check-generated-sdks.sh` | PASS | `rate-limit-sdk.log.txt` |
| `make fmt-check` | PASS, no output | `rate-limit-fmt.log.txt` |
| `docker compose --env-file .env config --quiet` | PASS, no output | `rate-limit-compose.log.txt` |

The test fixture uses only synthetic IP, tenant and principal values. No
credential, raw identity, database URL or production data is present in these
logs.
