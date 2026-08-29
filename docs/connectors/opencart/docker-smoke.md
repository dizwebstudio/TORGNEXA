# OpenCart: локальная проверка bridge в Docker Compose

В репозитории есть изолированный тестовый стенд OpenCart 4.1.0.4 + MariaDB. Он устанавливает официальный дистрибутив, подключает модуль `torgnexa.ocmod.zip` из исходников и загружает только синтетический каталог и заказ. Стенд не использует production credentials и не должен публиковаться в интернет.

## Требования

- Docker Engine с Compose v2;
- свободный TCP-порт `8095` (его можно изменить через `OPENCART_HTTP_PORT`);
- доступ Docker daemon к GitHub Releases для загрузки OpenCart во время сборки.

Версия OpenCart и SHA-256 архива зафиксированы в `docker/opencart-test/Dockerfile`. Это делает сборку воспроизводимой и не позволяет незаметно подменить архив.

## Запуск

Из корня репозитория:

```bash
docker compose -f docker-compose.opencart-test.yml up -d --build
docker compose -f docker-compose.opencart-test.yml ps
```

Первый запуск выполняет CLI-установку OpenCart, создаёт таблицы bridge и загружает демо-данные:

- `DEMO-COFFEE-001`, остаток 24, цена `1499.90` USD;
- `DEMO-TEA-002`, остаток 8, цена `799.00` USD;
- заказ `9001` с товарной строкой кофе.

Повторный `up` с сохранённым volume безопасен: entrypoint обнаруживает схему,
восстанавливает `config.php` и `admin/config.php`, а seed использует
идемпотентные вставки. Это позволяет пересобирать образ без повторной CLI-
установки и без потери демо-базы.

Локальные параметры стенда:

| Параметр | Значение |
| --- | --- |
| URL | `http://127.0.0.1:8095` |
| bridge token | `torgnexa-demo-bridge-token-2026` |
| MariaDB database | `opencart` |
| MariaDB user/password | `opencart` / `opencart-demo` |
| MariaDB root password | `opencart-root-demo` |

Все значения предназначены только для локальной синтетической проверки. Для production используйте отдельный secret manager и собственный модуль.

## Полный smoke-тест

После `healthy`-состояния контейнера выполните:

```bash
scripts/opencart-smoke.sh
```

Скрипт проверяет:

1. `401` без токена и `200` с токеном на health endpoint;
2. список товаров, пагинацию и наличие обоих демо-SKU;
3. поиск товара по SKU и чтение варианта;
4. изменение цены, безопасный повтор с тем же idempotency key и `409` для изменённого payload;
5. изменение остатка и чтение нового значения;
6. создание товара и повтор создания с тем же idempotency key;
7. список заказов и наличие заказа `9001`;
8. изменение статуса заказа и повторное чтение заказа.

Каждый запуск генерирует собственный префикс idempotency keys, поэтому повторный запуск не получает устаревший ответ из предыдущего теста. При успехе последняя строка вывода — `OpenCart bridge smoke: all checks passed`.

## Диагностика

```bash
docker compose -f docker-compose.opencart-test.yml logs --tail=200 opencart
docker compose -f docker-compose.opencart-test.yml logs --tail=200 db
curl -i -H 'Authorization: Bearer torgnexa-demo-bridge-token-2026' \
  'http://127.0.0.1:8095/index.php?route=extension/torgnexa/api/health'
```

Ожидаемый ответ health:

```json
{"ok":true,"api_version":"v1"}
```

Тестовый API-маршрут использует `product_code.code/value` с `code = 'SKU'`, как в OpenCart 4.1; старый `identifier_id` не поддерживается. Apache в тестовом образе явно передаёт `Authorization` в PHP, иначе bearer-заголовок теряется на границе веб-сервера.

## Остановка и очистка

Стенд содержит только демо-данные. После проверки остановите его и удалите volume базы:

```bash
docker compose -f docker-compose.opencart-test.yml down -v
```

Команда удаляет контейнеры и volume `opencart-demo-db`; исходники, архив bridge и production volumes не затрагиваются.
