# Epic 175 — Marketplace Advertising Runtime для WB и Ozon

## Статус

`repository-complete` для согласованного MVP read-контура. Пользовательский
номер Epic — `175`; в репозитории используется task key `220`, потому что
старые числовые ключи уже заняты.

## Что сделано

- **175.1–175.3** — зафиксирована capability-матрица `ads.read` для
  Wildberries и Ozon, добавлены provider-neutral факты и typed Connector SDK.
  В SDK уже описаны read/write operation names, idempotency, dry-run,
  accepted/rejected/unknown и нормализованные ошибки; write-маршруты пока не
  включены в runtime.
- **175.4–175.5** — добавлено чтение кампаний, дневных расходов, показов,
  кликов, заказов и выручки через официальные API-адаптеры WB/Ozon. Поля
  провайдера остаются внутри адаптера, наружу выходят только нормализованные
  записи с UTC-периодом, валютой, SKU и quality.
- **175.6** — worker ежедневно читает предыдущий полный UTC-день, хранит
  tenant-scoped sync run/watermark, повторяет безопасно по remote fact ID,
  сохраняет partial/unknown quality и не пишет секреты или raw responses.
- **175.7–175.8** — факты расходов подключены к существующему P&L. При наличии
  API-факта старые action/settlement-копии не суммируются повторно; источники
  остаются доступными для сверки. Для расхода или конверсии без SKU создаётся
  видимый `unattributed_*` finding, задержка помечается `delayed_report`, а
  изменённый исторический spend/performance — `changed_historical_report`.
- **175.10–175.11** — добавлены read-only API и generated SDK для кампаний,
  spend, performance, metrics, reconciliation и sync-runs. В интерфейсе
  появился раздел «Реклама» с периодом, каналом, кампанией, таблицей метрик,
  ROAS/ROMI/ДРР, расходом на заказ, статусом качества и findings.
- **175.12–175.13** — добавлены forced RLS, append-only facts/evidence,
  bounded queries, capability guard, синтетические domain/connector тесты,
  migration catalog, architecture review и Docker qualification checks.

## API и модель

Основные маршруты:

- `GET /api/v1/advertising/campaigns`;
- `GET /api/v1/advertising/spend`;
- `GET /api/v1/advertising/performance`;
- `GET /api/v1/advertising/metrics`;
- `GET /api/v1/advertising/reconciliation`;
- `GET /api/v1/advertising/sync-runs`.

Все суммы — integer minor units с ISO-валютой, коэффициенты — integer basis
points. PostgreSQL хранит кампании, immutable facts, findings и watermarks.
ClickHouse остаётся только будущей перестраиваемой аналитической проекцией.

## Сознательные ограничения

175.9 — запуск/остановка, ставки, бюджеты, товары кампании и batch-действия
описаны в SDK, но не допущены в production route. Они требуют отдельного
provider capability audit, policy/budget cap, approval, idempotency,
read-after-write и qualification неизвестного результата.

Связь orders/revenue выполняется по доступным provider facts и существующим
P&L order facts. Если провайдер не даёт однозначной SKU-атрибуции, значение
остаётся channel-level/unattributed и не превращается в ноль.

## Безопасность и эксплуатация

Токены читаются только через callback-scoped `SecretProvider`. В API, логах,
событиях, обычных таблицах и evidence нет токенов или raw provider responses.
Перед применением `000047_marketplace_advertising_runtime.sql` нужен проверенный
backup PostgreSQL; откат — отключение `ads.read` и drain worker, без
destructive down migration.

Официальные исходные контуры: [WB Promotion API](https://dev.wildberries.ru/openapi//promotion)
и [Ozon Performance API](https://docs.ozon.ru/api/performance/). Live
credentialed qualification остаётся release gate, синтетические проверки
входят в repository qualification.

## Зависимости

Tasks `050`, `167`, `174`, `217`, `218`, `219` и существующие connector
conformance/security boundaries.
