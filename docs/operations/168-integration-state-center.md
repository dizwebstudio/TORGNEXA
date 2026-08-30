# Task 168 — Единый центр состояния интеграций

## Что поставлено

Операторский маршрут `/integrations/status` показывает tenant-scoped снимок
каждого созданного кабинета. Он объединяет десять независимых измерений:

| Измерение | Что означает |
| --- | --- |
| Runtime | admitted production route: `ready`, отдельный раздел, health-only или не подключён |
| Кабинет | жизненный цикл аккаунта |
| Доступ | только безопасный класс credentials, без значений |
| Конфигурация | наличие и валидность non-secret runtime config |
| Подключение | последняя нормализованная health-проверка и её возраст |
| Операции | declared/granted/enabled/blocked/qualification-required |
| Синхронизация | policy/run состояние и retry |
| Сверка | открытые расхождения и failed run |
| Webhook | состояние host-owned delivery evidence |
| Лимит запросов | только нормализованный наблюдаемый rate-limit |

`overall` вычисляется серверным reducer. Приоритет: unsupported → blocked →
setup required → reauthorization required → disabled → degraded → stale →
attention → syncing → healthy → unknown. Вторичные проблемы остаются в
`issues`, поэтому здоровый транспорт не скрывает заблокированную запись или
открытое расхождение.

## API

```text
GET /api/v1/integration-center?limit=50&cursor=…&overall=attention&surface=logistics
GET /api/v1/integration-center/{account_id}
```

Оба запроса проходят OIDC → tenant resolver → `integrations.center.read`, не
принимают organization/workspace из query и не выполняют network probe. Ответ
содержит `snapshot_digest`, `generated_at`, `source_watermarks`, `partial`,
сводку и cursor. Для условного чтения используется ETag; чувствительные
снимки имеют `Cache-Control: no-store` от production composition.

Проверка подключения, OAuth, сохранение конфигурации, запуск синхронизации,
retry и reconciliation остаются в существующих mutation endpoints. Кнопка в
центре только ведёт к владельцу действия и не делает optimistic `healthy`.

## События и worker

Зарегистрированы canonical payloads:

- `commerce.integration.account_status_changed.v1`;
- `commerce.integration.snapshot_published.v1`.

Транспортный topic — `commerce.integration.events.v1`. Worker принимает
только metadata envelope, проверяет tenant scope и ставит coalesced работу в
`integration_center_recompute_queue` (уникальный ключ tenant/account). Лизинг,
attempt count и dead-letter статус ограничивают fan-out при сбое внешней
системы. В браузер через существующий SSE передаётся только invalidation; UI
повторно читает обычный API.

## PostgreSQL и безопасность

`000035_integration_state_center.sql` добавляет rebuildable snapshots,
account rows, immutable transitions/action receipts и durable recompute queue.
Все таблицы имеют composite organization/workspace key и `FORCE ROW LEVEL
SECURITY`; snapshot/action evidence нельзя UPDATE/DELETE. JSON-поля ограничены
размером и содержат только валидированный reducer output. Токены, secret
references, Authorization headers, raw provider errors и PII в центр не
попадают.

## Запуск и проверка

После обновления Compose применить миграции штатным bootstrap:

```bash
docker compose -f docker-compose.dev.yml up -d postgres kafka backend worker frontend
curl -H "Authorization: Bearer <session>" \
  'http://localhost:8080/api/v1/integration-center?limit=50'
```

Для synthetic demo создайте disabled account без secret, active account с
проверенным health evidence и policy с открытым drift. В UI ожидаются
`Нужно настроить`, `Работает`, `Заблокировано`, `Устарело` и `В отдельных
разделах`; health-only connector никогда не получает зелёную executable
операцию. Скриншоты следует снимать после seed synthetic data — production
credentials в evidence и screenshots запрещены.

Минимальные release gates:

```bash
go test ./...
go vet ./...
./scripts/check-contracts.sh
./scripts/check-migrations.sh
```

Frontend:

```bash
cd frontend
npm run typecheck
npm run test:logic
npm run build
```

При частичном источнике UI показывает предупреждение и сохраняет исходный
статус как `unknown/stale`; источник не превращается в healthy из-за ошибки
чтения. Для восстановления snapshot queue можно безопасно очистить только
derived projection и повторить recompute из authoritative таблиц.
