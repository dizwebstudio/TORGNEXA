# Закрытие 234.7 — tenant-scoped SSE broadcaster

Дата проверки: 2026-09-12. Решение:
[ADR-0196](../../adr/0196-tenant-scoped-sse-broadcaster.md).

## Реализованная граница

Каждый API-процесс создаёт один watcher на активную пару
organization/workspace. Первый audit-head lookup и последующий двухсекундный
poll делятся всеми SSE-клиентами этого tenant. Последний disconnect отменяет
watcher и удаляет tenant state. На другой реплике работает отдельный watcher.

Одновременно допускаются не более 64 клиентов одного tenant и 1024 клиентов
процесса. Превышение даёт `429` и `Retry-After: 5` до начала SSE. Очередь
клиента содержит один cursor: новый invalidation заменяет ожидающий старый.
Поэтому память не зависит от числа пропущенных audit changes, а медленная сеть
по-прежнему закрывается пятисекундным write/flush deadline.

Payload не менялся и содержит только `reason`, opaque `cursor` и UTC `at`.
Durable audit/outbox evidence остаётся источником сигнала; браузер после
connected/audit invalidation перечитывает обычные capability-protected API.
Broadcaster не хранит audit summaries, entity data или историю событий.

Label-free snapshot содержит active/peak clients, active tenants, admission
и rejection counters, audit-head queries/errors, published signals и
queued/coalesced deliveries. Tenant, identity и cursor не используются как
labels.

## Проверенные сценарии

- tenant и process caps, освобождение capacity после disconnect и `429` до
  `text/event-stream` headers;
- очередь размером один сохраняет только newest cursor при трёх быстрых
  invalidation;
- ошибка initial audit-head query учитывается без утечки деталей, а следующий
  bounded poll восстанавливает доставку;
- шесть reconnect-волн по 48 конкурентных handler: 288 подключений выполняют
  шесть audit-head lookup, по одному на волну, и не оставляют watchers;
- forced-RLS PostgreSQL: 32 клиента выполняют один initial query; после append
  immutable audit record один следующий query доставляет invalidation всем 32,
  итоговый query count равен двум;
- HTTP/1.1, HTTP/2, reconnect, heartbeat, cancel, periodic reauthorization и
  net.Pipe slow-consumer deadline остаются в regression suite.

## Результаты

- `gofmt` для изменённых Go-файлов — PASS.
- targeted realtime race tests — PASS.
- `./scripts/check-audit-postgres.sh` — PASS с forced RLS и `-race`.
- `./scripts/check.sh` — PASS: `go test ./...`, `go vet ./...`, contracts,
  SDK, architecture, migrations, supply-chain policy, frontend, package index
  и сборка.
