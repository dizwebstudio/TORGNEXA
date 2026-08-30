# CS-Cart connector specification

## Authentication

CS-Cart требует включить API access для администратора. В секрет интеграции
передаётся JSON:

```json
{"email":"admin@example.com","api_key":"<API key>"}
```

Runtime отправляет эти значения как HTTP Basic Auth: e-mail — username, API
key — password. Секрет доступен только в callback `SecretAccessor` и не
попадает в конфигурацию, логи или события.

## Runtime configuration

```json
{"store_host":"shop.example.com","base_path":"","store_currency":"RUB"}
```

`store_host` — публичное DNS-имя магазина без схемы; `base_path` — безопасный
путь установки, если магазин расположен не в корне; `store_currency` — ISO
4217 в верхнем регистре.

## API surface

Используется рекомендуемый CS-Cart API 2.0:

- `GET /api/2.0/products` — постраничное чтение каталога, базовых цен,
  `list_price` и `amount`;
- `GET /api/2.0/products/{product_id}` — проверка удалённого состояния;
- `GET /api/2.0/orders` — постраничное чтение заказов;
- `GET /api/2.0/orders/{order_id}` — чтение заказа и его строк;
- `POST /api/2.0/products` — создание товара;
- `PUT /api/2.0/products/{product_id}` — обновление товара.

Для повторяемости записи коннектор сначала ищет товар по `product_code`,
сравнивает нормализованное состояние и после записи выполняет read-after-write.
При неопределённом результате сети применяется та же reconciliation-политика.

Чтение цен использует ту же bounded-проекцию товаров: `price` становится
регулярной ценой, `list_price` — необязательной ценой до скидки, а
`product_id` — `variant_remote_id`. Поддержка option combinations и запись
цен намеренно не заявляются до появления отдельного проверенного контракта.
Остатки читаются по `amount` через `GET /api/2.0/products/{product_id}` и
публикуются в одной локации `cs-cart-store`; складская детализация не
выдумывается.

Заказы читаются из списка и затем перечитываются по ID для получения строк.
Буквенные статусы сопоставляются в runtime только для стандартных CS-Cart
кодов (`O`, `Y`, `P`, `B`, `C`, `I`, `F`, `D`); неизвестный код останавливает
проекцию. Строки с option combinations намеренно отклоняются, потому что
текущий нейтральный контракт не позволяет безопасно сохранить их remote
identity. API предоставляет время размещения, поэтому оно используется как
`created_at` и `updated_at`.

SDK-контракт и live-контракт проверяются раздельно. SDK-конформанс фиксирует
13/13 обязательных проверок, а credentialed HTTP-проверка настоящего магазина
выполняется `scripts/cscart-smoke.sh` по инструкции
`docs/connectors/cs-cart/docker-live-qualification.md`.
