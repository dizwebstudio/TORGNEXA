# Проверки A09 — 2026-09-10

Следующее продолжение: [A10 — SSE и браузерное восстановление](a10-validation.md).
Журналы A09 ниже сохранены для своего snapshot; A10 обновляет OpenAPI hash.

База рабочего дерева: `5bc91990e829fcce3d48935c66eb27ea324a7086` плюс предыдущие
исправления A01–A08 и изменения A09. Go 1.26.7, PostgreSQL 18.6, локальный image
`torgnexa-postgres:18.6-hardened-amd64`. Результат:
[A09 закрыта в репозитории](../2026-09-10-a09-fix.md).

Временный PostgreSQL запускается без сети и опубликованных портов; Go получает
доступ через Unix socket. Применяются все 62 миграции с проверкой checksum.
Application role — `NOSUPERUSER NOBYPASSRLS`, forced RLS включён. Admin нужен
только для синтетических фикстур, блокировок и failure injection. TLS UserInfo
работает локально и принимает только синтетический credential. Действующая БД
Community stack и реальные аккаунты не используются. Скрипт удаляет контейнер
и временный каталог после тестов.

## Журналы

- [До исправления](before.log.txt): ожидаемый exit 1, семь HTTP 401 вместо 204
  в `TestA09PostgresConcurrentOIDCAuthentication`. Этот прогон содержит только
  первый A09 regression и прежние A02–A08; он не является финальной проверкой.
- [API unit/contract + race](authentication.log.txt): exit 0, 3 верхнеуровневых
  теста, 14 вместе с подслучаями; классификация store errors и защита handler.
- [PostgreSQL + race](postgres.log.txt): exit 0, A02–A09 — 26 верхнеуровневых
  тестов, 65 с подслучаями; A09 — 5 верхнеуровневых, 7 с подслучаями.
  Нет FAIL, SKIP и DATA RACE. Синтетические WARN в соседних webhook-тестах
  относятся к ожидаемым отказам проверки/сохранения.
- [go test ./...](go-test.log.txt): exit 0, 215 пакетов ok.
- [go vet ./...](go-vet.log.txt): exit 0, без диагностик.
- [Контракты](contracts.log.txt): exit 0.
- [Архитектура](architecture.log.txt): exit 0.
- [Go/Python/TypeScript SDK](sdk.log.txt): exit 0, 358 public operations.
- [TypeScript declarations](sdk-types.log.txt): exit 0, без диагностик.

Пустые vet/types журналы означают успешную проверку без вывода. Общий Go test
использовал обычное кеширование; отдельные API и PostgreSQL тесты выполнены
с `-count=1 -race`. Без переменных тестовой БД обычный Go suite пропускает
DB integration; полный PostgreSQL gate выше выполнил их без пропусков.
SDK-скрипт не нашёл глобальный `tsc`; декларации проверены отдельно через
`frontend/node_modules/typescript/bin/tsc`.

## Команды

```bash
export PATH=/home/mikhail/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.7.linux-amd64/bin:$PATH
export GOTOOLCHAIN=local GOTELEMETRY=off GOCACHE=/tmp/torgnexa-audit-go-cache
go test -count=1 -race -v ./internal/app/api -run 'TestA09OIDCSessionFailureClassification|TestA09SessionOutageContract|TestProductionCompositionFailsClosedAtEachAuthorizationStage'
TORGNEXA_TEST_POSTGRES_IMAGE=torgnexa-postgres:18.6-hardened-amd64 ./scripts/check-audit-postgres.sh
go test ./...
go vet ./...
./scripts/check-contracts.sh
./scripts/check-architecture.sh
./scripts/check-generated-sdks.sh
node frontend/node_modules/typescript/bin/tsc -p sdk/typescript/tsconfig.json
git diff --check
```

OpenAPI SHA-256 после обновления shared 503 contract:
`415d41f1717ea86ff132405784c51b6804f66c1717129293f637720b6c37dbfc`.
Generated SDK manifest совпадает с этим hash. Gofmt выполнен для семи
изменённых/добавленных Go-файлов A09. Миграции и frontend-код A09 не менялись.
Живой IdP, production load и развёртывание этим прогоном не проверялись.
