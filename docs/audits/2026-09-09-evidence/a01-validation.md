# Проверки A01 — 2026-09-09

Рабочее дерево на основе `5bc91990e829fcce3d48935c66eb27ea324a7086`.
Node 24.20.0, Chrome 152.0.7977.82, Go 1.26.7; зависимости frontend из lockfile:
React 19.2.8, TanStack Query 5.101.4, Vite 8.0.16, TypeScript 7.0.2.
Все итоговые прогоны ниже завершились с exit 0. Фикстуры синтетические.

| Журнал | Команда |
| --- | --- |
| [Браузер](a01-browser.log.txt) | `node scripts/check-auth-cache-browser.mjs` |
| [Session controller: 10](a01-session-tests.log.txt) | `node frontend/test/session-isolation.test.mjs` |
| [OIDC adapter: 7](a01-oidc-tests.log.txt) | `node frontend/test/keycloak-session-race.test.mjs` |
| [API retry: 5](a01-api-tests.log.txt) | `node frontend/test/auth-retry.test.mjs` |
| [Все logic-файлы](a01-logic.log.txt) | `npm --prefix frontend run test:logic` |
| [Полный frontend gate](a01-frontend-full.log.txt) | `./scripts/check-frontend-shell.sh` |
| [Go tests](a01-go-test.log.txt) | `go test ./...` |
| [Go vet](a01-go-vet.log.txt) | `go vet ./...` |
| [Контракты](a01-contracts.log.txt) | `./scripts/check-contracts.sh` |
| [Архитектура](a01-architecture.log.txt) | `./scripts/check-architecture.sh` |

Перед прямым запуском `.test.mjs` нужен
`frontend/node_modules/.bin/tsc -p frontend/tsconfig.logic.json`; npm logic script
делает это автоматически. Прямые прогоны сохраняют отдельные assertions/TAP
счётчики, поскольку общий Node runner в этом окружении выводит итоги по файлам.

Браузерный gate также доступен через `make frontend-auth-cache-check` или
`npm --prefix frontend run test:auth-cache`. Нужны установленные frontend
dependencies и Chrome/Chromium; `CHROME_BIN` задаёт путь к браузеру. Vite и
headless Chrome используют временные loopback ports и отдельный профиль;
running Community stack и реальные IdP/API не вызываются. После проверки
созданные процессы и профиль удаляются.

Первый браузерный прогон остановился на ожидании имени B: стандартная узкая
ширина headless Chrome скрывала профиль через responsive CSS. Runner получил
фиксированный viewport 1440×1100; последующие прогоны успешны. Первый запуск
architecture gate не нашёл Go в PATH; с закреплённым Go 1.26.7 он прошёл.
Это ошибки окружения/сценария, а не скрытые пропуски проверок.

Для Go использовались `GOTOOLCHAIN=local`, `GOTELEMETRY=off` и отдельный
`GOCACHE=/tmp/torgnexa-audit-go-cache`. Go tests используют обычное кеширование;
сокеты локальных HTTP-тестов разрешены. A01 не меняет Go/SQL и не требует нового
PostgreSQL migration/replay прогона.
