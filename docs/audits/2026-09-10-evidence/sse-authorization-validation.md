# Проверки повторной авторизации SSE — 2026-09-10

База: `9524d8c5220a075626d31d04c83ff5a900070a73` плюс это исправление.
Go 1.26.7; локальные HTTP/TLS-серверы; отдельная PostgreSQL 18.6 с полным
каталогом из 62 миграций и ролью `audit_integration` NOSUPERUSER/NOBYPASSRLS;
отдельный headless Chrome с временным профилем. Все fixtures синтетические.
[Результат](../2026-09-10-sse-authorization-fix.md).

## Журналы

- [До исправления](sse-authorization-before.log.txt): ожидаемый exit 1;
  после отзыва permission HTTP-поток дожил до client timeout 17 секунд.
  После исправления тот же сценарий использует ускоренный check interval.
- [SSE HTTP/unit + race](sse-authorization-unit.log.txt): exit 0, 13
  верхнеуровневых тестов / 45 с подслучаями; новые authorization tests 6 / 26,
  прежние A10/metadata tests 7 / 19. Нет FAIL, SKIP или DATA RACE.
- [PostgreSQL + race](sse-authorization-postgres.log.txt): exit 0, 27 / 71;
  все A02–A09 и пять новых подслучаев TestRealtimePostgresReauthorization.
  Нет FAIL, SKIP или DATA RACE. Gate расширен, чтобы включать SSE в повторы.
- [go test ./...](sse-authorization-go-test.log.txt): exit 0, 215 пакетов ok.
- [go vet ./...](sse-authorization-vet.log.txt): без диагностик.
- [Контракты](sse-authorization-contracts.log.txt): PASS.
- [Архитектура](sse-authorization-architecture.log.txt): PASS.
- [SDK Go/Python/TypeScript](sse-authorization-sdk.log.txt): PASS,
  358 операций; [TypeScript declarations](sse-authorization-sdk-types.log.txt)
  скомпилированы отдельно без диагностик, поскольку глобальный tsc отсутствует.
- [Frontend gate](sse-authorization-frontend.log.txt): exit 0, 9 test files,
  types/static policy, production build и prerender.
- [Chrome SSE](sse-authorization-browser.log.txt): exit 0, четыре сценария
  reconnect/heartbeat/coalescing/logout.

Пустые vet/types журналы означают отсутствие диагностик. Полный Go suite
использует обычное кеширование; целевые HTTP и PostgreSQL gates явно запущены
с `-count=1 -race`. Требуемые `gofmt` и `git diff --check` также выполнены.

## Команды

```bash
export PATH=/home/mikhail/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.7.linux-amd64/bin:$PATH
export GOTOOLCHAIN=local GOTELEMETRY=off GOCACHE=/tmp/torgnexa-audit-go-cache
go test -count=1 -race -v ./internal/app/api -run 'TestRealtimeAuthorization|TestA10|TestRealtimeStream'
TORGNEXA_TEST_POSTGRES_IMAGE=torgnexa-postgres:18.6-hardened-amd64 ./scripts/check-audit-postgres.sh
go test ./...
go vet ./...
./scripts/check-contracts.sh
./scripts/check-architecture.sh
./scripts/check-generated-sdks.sh
frontend/node_modules/.bin/tsc -p sdk/typescript/tsconfig.json
./scripts/check-frontend-shell.sh
node scripts/check-realtime-browser.mjs
git diff --check
```

PG gate создаёт временный container с `--network none`, Unix socket и tmpfs,
проверяет migration hashes, применяет каталог, запускает тесты и удаляет
container. Для ошибок прав используются только таблицы и application role
этой временной БД; рабочая PostgreSQL не меняется.

В HTTP/PG тестах интервалы повторной авторизации уменьшены до 50–80 мс.
Отдельные expiry tests ставят следующую проверку через час: независимый
deadline токена 300 мс закрывает HTTP/1.1/2. В net.Pipe expiry 500 мс должен
прервать blocked write раньше пятисекундного бюджета. Проверки timeouts
используют context cancellation, а не внешний client timeout как путь успеха.
Production: recheck 15 с, общий check timeout 5 с, poll 2 с, heartbeat 15 с,
frame write budget 5 с с ограничением исходным credential expiry.

OpenAPI SHA-256:
`16a5af13a334fbabd0e04a175447210bfa743c3fb97270ffbda27d6d79dd8317`.
Generated SDK manifest соответствует этому snapshot; предыдущий A10 hash
описывает состояние до этого продолжения. Миграции не менялись.
