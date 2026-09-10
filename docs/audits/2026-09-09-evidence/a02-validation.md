# Проверки A02 — 2026-09-09

База рабочего дерева: `5bc91990e829fcce3d48935c66eb27ea324a7086` плюс
исправления A01–A08. Go 1.26.7, PostgreSQL 18.6, локальный image
`torgnexa-postgres:18.6-hardened-amd64`; Node 24.20.0, TypeScript 7.0.2.
Все перечисленные итоговые команды завершились с exit 0.

| Журнал | Команда / результат |
| --- | --- |
| [A02 unit/contract](a02-unit.log.txt) | `go test -count=1 -race -v ./internal/app/api -run 'TestA02(OIDC\|Tenant\|Invitation)'` — 3 / 15 с подслучаями |
| [A02–A08 PostgreSQL](a02-postgres.log.txt) | `TORGNEXA_TEST_POSTGRES_IMAGE=torgnexa-postgres:18.6-hardened-amd64 ./scripts/check-audit-postgres.sh` — 21 / 58 с подслучаями |
| [Go tests](a02-go-test.log.txt) | `go test ./...` — 215 пакетов ok |
| [Go vet](a02-go-vet.log.txt) | `go vet ./...` — без диагностик |
| [Контракты](a02-contracts.log.txt) | `./scripts/check-contracts.sh` |
| [Архитектура](a02-architecture.log.txt) | `./scripts/check-architecture.sh` — 168 modules, 61 providers, 220 reviews |
| [Генерация SDK](a02-sdk-generate.log.txt) | `go -C tools/sdkgen run . --root ../..` |
| [Проверки SDK](a02-sdk.log.txt) | `./scripts/check-generated-sdks.sh` — Go, Python, TypeScript runtime, source hash/inventory |
| [TypeScript declarations](a02-sdk-types.log.txt) | `node frontend/node_modules/typescript/bin/tsc -p sdk/typescript/tsconfig.json` |

Для точного повторения unit-команды из таблицы используйте regex
`TestA02(OIDC|Tenant|Invitation)`; обратные слеши у `|` в Markdown нужны только
для отображения таблицы. Закреплённый Go доступен в
`/home/mikhail/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.7.linux-amd64/bin`.
В запусках использовались `GOTOOLCHAIN=local`, `GOTELEMETRY=off`,
`GOCACHE=/tmp/torgnexa-audit-go-cache`.

Database gate создаёт временный контейнер без сети/опубликованных портов,
применяет весь migration catalog с checksum validation и выдаёт приложению
`NOSUPERUSER NOBYPASSRLS` роль. Application pool ограничен одним соединением;
application calls выполняются под forced RLS. Unix socket, контейнер и
временный каталог удаляются после прогона. Рабочая Community БД не используется.

A02: 12 вариантов UserInfo, 3 existing/disabled-member варианта и один
cross-workspace сценарий, включая successful/rejected replay и сравнение
полной записи до/после отказа. UserInfo — локальный TLS fixture с синтетическими
данными. Настоящие provider credentials или внешние аккаунты не используются.
Проверяется действительная композиция API и SQL, а не поддельный role store.

В общем PostgreSQL логе ожидаемые WARN/ERROR из старых webhook failure-injection
сценариев A03–A08 не означают провал A02. Нет FAIL, SKIP или data race.
Обычный Go test использует кеш; database и целевой unit run явно отключают его.

SDK gate не нашёл глобальный `tsc`; поэтому TypeScript declarations проверены
отдельно закреплённым компилятором. OpenAPI 0.21.2, 358 операций, SHA-256:
`f892e08a4de0918b0df8cea922277f5193052aad2b3b79d8cd177548126f5b32`.
