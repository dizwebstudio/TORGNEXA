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

- `GET /api/2.0/products` — постраничное чтение каталога;
- `GET /api/2.0/products/{product_id}` — проверка удалённого состояния;
- `POST /api/2.0/products` — создание товара;
- `PUT /api/2.0/products/{product_id}` — обновление товара.

Для повторяемости записи коннектор сначала ищет товар по `product_code`,
сравнивает нормализованное состояние и после записи выполняет read-after-write.
При неопределённом результате сети применяется та же reconciliation-политика.
