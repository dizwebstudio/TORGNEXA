# Аудит возможностей «Деловых Линий»

Официальная документация подтверждает два способа авторизации Public API:
по номеру телефона и паролю (`v3/auth/login.json`) либо по appkey и
персональному токену (`v4/auth/login.json`). Для TORGNEXA выбран PAT: он не
требует хранить пароль и действует ограниченный срок, заданный перевозчиком.

Подтверждённые источники:

- [Авторизация пользователя](https://dev.dellin.ru/api/auth/login/) — appkey + PAT и временный sessionID;
- [Калькулятор стоимости](https://dev.dellin.ru/api/calculation/calculator/) — `v2/calculator.json`;
- [Оформление заказа](https://dev.dellin.ru/api/examples/request/) — `v2/request.json`;
- [Журнал заказов](https://dev.dellin.ru/api/orders/search/) — `v3/orders.json`;
- [История статусов заказа](https://dev.dellin.ru/api/orders/statuses-history/) — `v3/orders/statuses_history.json`;
- [Справочник терминалов](https://dev.dellin.ru/api/terminals/directory/) — `v3/public/terminals.json`.

В текущем runtime включены credential-проверка, bounded read-only чтение
справочника терминалов/ПВЗ, `logistics.rates.read` и единичное
`logistics.track.read`. Rates использует официальный калькулятор с текстовыми
адресами, агрегированными габаритами и весом не более 50 мест; `freightUID` не
передаётся, поскольку для этого bounded preview он необязателен. Tracking
использует официальный `POST /v3/orders/statuses_history.json` с `appkey` и
одним `docIds`, ограничивает историю 100 событиями, выбирает последнюю дату и
не переносит сырой ответ или данные клиента в Core. Оформление, отмена,
этикетки и запись требуют отдельных обезличенных fixtures, маппинга
адресов/терминалов и подтверждения повторяемости create/status на одном
idempotency key.
