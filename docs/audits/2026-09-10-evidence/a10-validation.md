# Проверки A10 — 2026-09-10

База рабочего дерева: `5bc91990e829fcce3d48935c66eb27ea324a7086` плюс предыдущие
исправления A01–A09 и изменения A10. Использованы Go 1.26.7, локальные TLS
HTTP/1.1 и HTTP/2 серверы, отдельный headless Chrome с временным профилем.
[Результат](../2026-09-10-a10-fix.md).

## Журналы

- [До исправления](a10-before.log.txt): ожидаемый exit 1, оба HTTP-протокола
  теряют поток после общего write deadline. Перед этим прогоном только вынесены
  интервалы в параметры тестовой сборки маршрута; исправление ещё отсутствует.
- [SSE Go tests + race](a10-realtime.log.txt): exit 0, 7 верхнеуровневых тестов,
  19 с подслучаями. A10 — 5/17; ещё два проверяют прежние metadata/fast-path
  гарантии. Нет FAIL, SKIP и DATA RACE.
- [Браузер](a10-browser.log.txt): exit 0, четыре сценария — liveness без
  refetch, восстановление после reconnect, объединение восьми invalidate,
  прекращение SSE/retry/refetch после logout.
- [go test ./...](a10-go-test.log.txt): exit 0, 215 пакетов ok.
- [go vet ./...](a10-go-vet.log.txt): exit 0, без диагностик.
- [Контракты](a10-contracts.log.txt): exit 0.
- [Архитектура](a10-architecture.log.txt): exit 0.
- [SDK Go/Python/TypeScript](a10-sdk.log.txt): exit 0, 358 public operations.
- [TypeScript declarations](a10-sdk-types.log.txt): exit 0, без диагностик.
- [Frontend gate](a10-frontend.log.txt): exit 0, 9 test files, type checks,
  static policy, production build и prerender.

Пустые vet/types журналы означают отсутствие диагностик при успешном выходе.
Полный Go suite использует обычное кеширование; SSE regression явно запущен
с `-count=1 -race`. Глобальный `tsc` не установлен, поэтому SDK declarations
скомпилированы отдельно через установленный frontend compiler.

## Команды

```bash
export PATH=/home/mikhail/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.7.linux-amd64/bin:$PATH
export GOTOOLCHAIN=local GOTELEMETRY=off GOCACHE=/tmp/torgnexa-audit-go-cache
go test -count=1 -race -v ./internal/app/api -run 'TestA10|TestRealtime'
node scripts/check-realtime-browser.mjs
go test ./...
go vet ./...
./scripts/check-contracts.sh
./scripts/check-architecture.sh
./scripts/check-generated-sdks.sh
node frontend/node_modules/typescript/bin/tsc -p sdk/typescript/tsconfig.json
./scripts/check-frontend-shell.sh
git diff --check
```

У браузерного сценария есть алиас `npm --prefix frontend run test:realtime`.
Он запускает локальный Vite и отдельный Chrome, удаляет временный профиль,
использует синтетические данные и не обращается к рабочему API/IdP.

Основной HTTP regression уменьшает интервалы: общий WriteTimeout 80 мс,
budget кадра 100 мс, heartbeat 300 мс и audit poll 20 мс. Два idle-интервала
доказывают, что ни общий, ни кадровый deadline не остаются активными между
кадрами. Production сохраняет poll 2 с, heartbeat 15 с и budget кадра 5 с.
Медленный клиент проверен отдельным настоящим http.Server с синхронным
net.Pipe и бюджетом кадра 120 мс, чтобы не зависеть от размеров TCP-буферов.

OpenAPI SHA-256 после A10:
`d9899258ae0e78244fb069a6e9ac6955add64374238a2c1792ee7dd19751b219`.
Generated SDK manifest соответствует этому snapshot. Ранее сохранённый
A09 SHA в README относится к состоянию до A10.

Миграции и DB-операции не менялись; PostgreSQL gate A02–A09 в этом продолжении
не повторялся. Живой IdP/DB/proxy stack, production capacity и развёртывание
этими проверками не заявляются.
