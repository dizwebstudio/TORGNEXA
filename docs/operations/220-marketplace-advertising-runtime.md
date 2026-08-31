# Реклама маркетплейсов: эксплуатация

## Назначение

Контур загружает read-only статистику рекламы WB и Ozon, нормализует её в
кампании, расходы и performance facts и передаёт расходы в существующий P&L.
Провайдерские ответы не становятся частью обычной модели данных.

## Рабочий цикл

Worker выбирает активные tenant/workspace scopes и аккаунты marketplace с
включённой capability `ads.read`. Для каждого аккаунта загружается предыдущий
полный UTC-день. Run получает `queued → running → completed|partial`; в нём
сохраняются только bounded counters, watermark, status/error code и digest.
Повтор дневного запуска безопасен: одинаковый account/period/mode не создаёт
новый run, а одинаковый remote fact не дублируется.

Статистика с задержкой или неполным набором данных не подменяется нулём.
Расход без SKU создаёт `unattributed_spend`, конверсия без SKU —
`unattributed_performance`; delayed/partial/unknown отмечается как
`delayed_report`.

## API

Все маршруты требуют `ads.read` и работают только в authenticated
organization/workspace scope:

- `GET /api/v1/advertising/campaigns?channel=&campaign_id=&limit=`;
- `GET /api/v1/advertising/spend?from=&to=&channel=&campaign_id=&sku=&limit=`;
- `GET /api/v1/advertising/performance?from=&to=&channel=&campaign_id=&sku=&limit=`;
- `GET /api/v1/advertising/metrics?from=&to=&channel=&campaign_id=&sku=&limit=`;
- `GET /api/v1/advertising/reconciliation?limit=`;
- `GET /api/v1/advertising/sync-runs?account_id=&limit=`.

ROAS, ROMI и ДРР возвращаются в basis points; денежные значения — в minor
units. Метрики строятся из сохранённых фактов и не вызывают провайдера во время
запроса UI.

## P&L и сверка

При наличии подтверждённых API-фактов P&L использует их как источник
advertising spend, удаляя из расчётного input только старые action/settlement
копии этого вида расхода. Исходные settlement и advertising rows не меняются
и остаются для будущего provider-total vs local-total reconciliation. Таким
образом, расхождение не скрывается пересчётом и не создаёт двойной расход.

## Секреты и диагностика

Секреты передаются только в callback `SecretProvider` и не логируются. В логах
разрешены scope IDs, длительность, статус и нормализованный error code. Raw
HTTP bodies, Authorization headers, токены и credential bundles запрещены.

При `partial` сначала проверяйте `/advertising/reconciliation`, затем health
аккаунта и watermark в `/advertising/sync-runs`. Повтор запускает следующий
безопасный daily pass; ручной backfill должен быть отдельным worker/API
расширением и не должен редактировать immutable fact.

Перед применением migration 47 требуется проверенный backup PostgreSQL. При
инциденте отключите `ads.read`, остановите/drain worker и оставьте факты
неизменяемыми. Управление кампаниями, бюджетами и ставками намеренно не
доступно до отдельной approval-bound qualification.
