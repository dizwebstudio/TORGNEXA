# Проверки исправлений A05–A08 — 2026-09-09

Дополнительное продолжение: [проверки A01 и браузерного кеша](a01-validation.md).
Принятие приглашений: [проверки A02, UserInfo и PostgreSQL](a02-validation.md).

База рабочего дерева: `5bc91990e829fcce3d48935c66eb27ea324a7086` плюс изменения
этого продолжения. Go 1.26.7, PostgreSQL 18.6; image
`torgnexa-postgres:18.6-hardened-amd64`. Тестовая БД создаётся заново из всех
62 миграций, без сети и опубликованных портов. Application role:
`NOSUPERUSER NOBYPASSRLS`; admin используется только для фикстур и injected
failure trigger. Все данные синтетические, реальные сервисы не вызываются.

Все перечисленные проверки завершились с exit 0. При первом архитектурном
прогоне test-only provider table был размещён в API; тест заменён проверкой
через общий интерфейс, после чего architecture gate прошёл. В PostgreSQL
логе WARN `secrets: consumer failed` ожидается при проверке неверной подписи.

- [PostgreSQL + race: 9 сценариев / 18 с подслучаями](postgres-race.log.txt)
- [go test ./...: 215 пакетов ok](go-test.log.txt)
- [go vet ./...: отсутствие диагностик](go-vet.log.txt)
- [Контракты](contracts.log.txt)
- [Архитектура](architecture.log.txt)
- [Миграции и baseline](migrations.log.txt)
- [Go/Python/TypeScript SDK](sdk.log.txt)
- [TypeScript compile: отсутствие диагностик](sdk-types.log.txt)
- [Финальная проверка общего API-интерфейса webhook](webhook-unit.log.txt)

`check-generated-sdks.sh` не нашёл глобальный `tsc`; проверка деклараций отдельно
выполнена командой `node frontend/node_modules/typescript/bin/tsc -p
sdk/typescript/tsconfig.json` и завершилась успешно. Полный Go test содержит
обычное кеширование; PostgreSQL-скрипт явно использует `-count=1 -race`.

Воспроизведение: `TORGNEXA_TEST_POSTGRES_IMAGE=<локальный-image>
./scripts/check-audit-postgres.sh`. Скрипт удаляет созданный контейнер и
временный каталог по завершении. Ни токены, ни реальные callback references
в журналах не сохранены.
