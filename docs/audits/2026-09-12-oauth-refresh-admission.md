# Закрытие 234.5 — OAuth refresh admission и метрики

Дата проверки: 2026-09-12. Решение: [ADR-0194](../../adr/0194-bounded-oauth-refresh-admission.md).

## Реализованная граница

`secretrepo.Repository` создаёт один semaphore на API/worker процесс. Все
создаваемые для аккаунтов TokenManager используют этот общий coordinator, даже
если сами manager создаются на каждый runtime call. Лимит равен восьми либо
меньше, если pool мал; при `MaxOpenConns>1` один slot остаётся вне refresh
budget. При pool=1 поддерживается один refresh без nested transaction.

Retry охватывает только `pg_try_advisory_xact_lock`: exponential ceiling растёт
от 25 до 250 ms, а фактическая пауза случайна в пределах 50–100% ceiling. OAuth
POST не находится в retry loop. Это сохраняет at-most-one attempt для
неоднозначных ответов провайдера и совместимо с rotating refresh tokens.

Label-free process snapshot содержит admission limit/current/peak/waiters,
wait/cancel counters и histogram, lock attempts/contentions/wait histogram,
refresh success/failure и end-to-end latency histogram, а также
`database/sql.DBStats` и current/peak saturation PPM. Refresh считается
успешным только после успешного commit транзакции ротации. Ни один показатель
не принимает tenant, account, connector, endpoint, reference, credential,
payload или raw error как label.

Размер PostgreSQL pool не изменён. Поэтому требование о временном повышении
минимума неприменимо; startup по-прежнему валидирует существующий
`TORGNEXA_DB_MAX_OPEN_CONNS`, а admission budget выводится из него при создании
repository.

## Интеграционные сценарии

`./scripts/check-audit-postgres.sh` запускает полный migration catalog в
одноразовом network-none PostgreSQL, использует непривилегированную роль под
forced RLS и выполняет connector-auth сценарии с `go test -race`.

- pool=1 и pool=4: конкурентные вызовы завершаются без self-deadlock, каждый
  аккаунт обновляется один раз, соединения возвращаются в pool;
- pool=12 и десять истёкших аккаунтов: одновременно входят ровно восемь,
  остальные видны как admission waiters, измеряется peak saturation;
- два конкурентных выполнения одного reference: второе получает try-lock
  contention, ждёт backoff и входит после освобождения;
- rejected refresh и cancellation не меняют secret version и учитываются как
  failure;
- durable intent failure не вызывает provider, rollback не оставляет lock или
  частичную ротацию.

## Результаты

- `gofmt` для изменённых Go-файлов — PASS.
- `go test ./internal/platform/postgres/secretrepo ./internal/platform/connectorauth` — PASS.
- `./scripts/check-audit-postgres.sh` — PASS с `-race`, включая два новых и
  один расширенный PostgreSQL-сценарий 234.5.

Полный repository gate (`go test ./...`, `go vet ./...`, contracts,
architecture и package index) выполняется в том же change перед отправкой.
