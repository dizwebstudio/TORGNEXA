# Task 234.10 — security/performance regression and load gates

Дата: 2026-09-13. Статус репозитория: complete.

## Что закреплено

`scripts/check-audit-postgres.sh` поднимает одноразовый PostgreSQL, применяет
полный migration catalog, использует отдельную unprivileged role с forced RLS и
запускает `-race` suite для 234.1–234.5. Last-admin сценарий одновременно меняет
двух разных администраторов: ровно одна транзакция проходит, вторая получает
`ErrLastAdministrator`; отдельно проверены demotion, disable, корректный replay
и конфликт того же idempotency key с другим payload.

Тот же gate проверяет atomic payment/audit/webhook и OAuth refresh failure
сценарии. Go test JSON остаётся во временном каталоге. В сохраняемый
`postgres-regression.json` попадают только имена и статусы сценариев, агрегаты
auth/DB/IdP, OAuth pool saturation и SSE broadcaster; каталог удаляется после
run.

Повторные CI-запуски выявили ошибку наблюдаемости в момент передачи OAuth
admission slot: старый код сначала освобождал slot и только затем уменьшал
`in_flight`, поэтому следующий waiter мог записать ложный peak выше лимита.
Release теперь сначала исключает завершившуюся операцию из `in_flight`, затем
делает slot доступным. Отдельные assertions проверяют concurrency limit, peak,
refresh successes/failures и latency count; после исправления полный
PostgreSQL gate пять раз подряд прошёл под `-race`.

Release qualification оставляет health probe отдельным и дополнительно получает
короткоживущий JWT из одноразового Keycloak realm. Direct grant включается только
в этом disposable project. `runtime-load.py` удерживает 32 авторизованных SSE
клиента и одновременно распределяет 1200 GET по workspace, members и audit API.
Token хранится в файле mode `0600`, не передаётся аргументом процесса и удаляется
до формирования evidence.

## Supply-chain gate

Gosec и govulncheck запускаются отдельно для root, Go SDK, Go SDK example,
contractcheck и sdkgen. Root gosec исключает `sdk/` и `tools/`, поэтому nested
modules не сканируются дважды. Package-less `tools/securitytools` остаётся
проверенным carrier модулем; появление в нём `.go` source требует явного
добавления в scan inventory.

Trivy secret findings проходят классификацию до редактирования. Synthetic
fixture разрешён только при полном совпадении target, RuleID и SHA-256 raw match
с `supply-chain/synthetic-secret-fixtures.json`; сейчас список пуст. Любая иная
находка получает класс `credential_candidate` и блокирует gate. Raw match,
secret, code и snippet удаляются из retained JSON.

## Сохраняемый отчёт

`.github/workflows/ci.yml` сохраняет `postgres-regression.json` на 30 дней.
Release workflow сохраняет весь qualification artifact на 90 дней, включая
`security-performance-regression.json`. Итоговый файл содержит:

- exact Git commit и pinned scanner versions;
- p50/p95/p99 для authenticated HTTP, auth security path, OAuth refresh и SSE
  connect;
- total/max DB и IdP calls, throttled `last_seen_at` writes;
- OAuth concurrency, wait и peak PostgreSQL pool saturation;
- SSE peak clients/query fan-out;
- passed PostgreSQL rollback/concurrency/replay и runtime restart/outage drills.

Report schema исключает токены, credentials, raw responses/logs, identity values
и tenant labels. Он доказывает regression behavior на CI/release topology, но не
заменяет capacity qualification целевой production topology.

## Проверка

Полный disposable production qualification прошёл 2026-09-13. Health sample:
500/500, p99 18.9 ms, 3156.9 req/s. Authenticated mix: 1200/1200, p50 280.8 ms,
p95 511.0 ms, p99 832.3 ms, 104.4 req/s. SSE: 32/32 одновременно подключённых
клиента, p99 connect 281.2 ms. Worker, Kafka и PostgreSQL restart/recovery drills
прошли; итоговый отчёт сохранил 73 regression/failure результата.

Qualification дополнительно выявил несовместимость hardened Postgres image с
root-owned новым volume `garage-config`. Одноразовый config writer теперь имеет
только необходимый root UID, отключённую сеть, пустой capability set и
`no-new-privileges`; Garage runtime читает созданный файл `0600` через read-only
mount.

Общие `go test ./...`, `go vet ./...`, contracts, architecture, migration,
policy, SDK, frontend build/tests и repository performance gate прошли. Pinned
gosec не нашёл High/Critical; исправлены Slowloris servers и точечно обоснована
сериализация только synthetic/encrypted-store credentials и provider-required
legacy MD5. Pinned govulncheck для пяти source modules не нашёл reachable
findings. Trivy secret scan не нашёл credential candidates или fixtures;
misconfiguration scan не нашёл High/Critical.
