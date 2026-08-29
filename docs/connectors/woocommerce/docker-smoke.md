# WooCommerce: локальная проверка в Docker Compose

В репозитории есть изолированный стенд WordPress 6.8.2 + WooCommerce 9.8.5 + MariaDB. Он устанавливает официальный WooCommerce REST API v3, создаёт только синтетические товары и заказ и выдаёт отдельную тестовую пару Consumer Key/Consumer Secret. Production-секреты и реальные данные в стенд не попадают.

## Требования

- Docker Engine с Compose v2;
- свободные TCP-порты `8096` (локальная витрина по HTTP) и `8446` (REST API по TLS), либо свои значения через `WOOCOMMERCE_HTTP_PORT` и `WOOCOMMERCE_HTTPS_PORT`;
- доступ Docker daemon к Docker Hub, GitHub Releases и downloads.wordpress.org во время сборки.

Образы и версии зафиксированы в `docker-compose.woocommerce-test.yml` и
`docker/woocommerce-test/Dockerfile`. TLS в стенде самоподписанный и подходит
только для локальной проверки; smoke-скрипт использует `--insecure`, а в
production транспорт обязан проверять сертификат удалённого магазина.

## Запуск

Из корня репозитория:

```bash
docker compose -f docker-compose.woocommerce-test.yml up -d --build
docker compose -f docker-compose.woocommerce-test.yml ps
```

Первый запуск выполняет установку WordPress, активирует WooCommerce, создаёт
REST API key с правами `read_write`, страницу магазина и демо-данные:

- `TORGNEXA-WOO-COFFEE`, остаток 24, цена `1499.90` USD;
- `TORGNEXA-WOO-TEA`, остаток 8, цена `799.00` USD;
- один синтетический заказ с двумя единицами кофе.

Витрина доступна по адресу `http://127.0.0.1:8096/shop/`, а REST API
проверяется по TLS: `https://127.0.0.1:8446/wp-json/wc/v3`. В образ добавлен
только локальный фильтр, который убирает WordPress-кэноникальный redirect для
`/wp-json/`, чтобы HTTPS API не перенаправлялся на HTTP-витрину. В production
этот стендовый фильтр не используется.

Локальные тестовые параметры:

| Параметр | Значение |
| --- | --- |
| витрина | `http://127.0.0.1:8096` |
| REST API | `https://127.0.0.1:8446/wp-json/wc/v3` |
| Consumer Key | `ck_torgnexa_demo_20260829_000000000000000000` |
| Consumer Secret | `cs_torgnexa_demo_20260829_0000000000000000` |
| валюта магазина | `USD` |
| MariaDB database | `wordpress` |
| MariaDB user/password | `wordpress` / `wordpress-demo` |

Значения синтетические и предназначены только для локального smoke-теста.

## Полный smoke-тест

После состояния `healthy` выполните:

```bash
scripts/woocommerce-smoke.sh
```

Скрипт проверяет:

1. отказ без Basic Auth (`401`) и успешный доступ с Consumer Key/Secret;
2. список товаров, наличие обоих демо-SKU и поиск по SKU;
3. изменение названия, цены и управляемого остатка товара с последующим чтением;
4. список заказов, изменение статуса заказа и повторное чтение;
5. endpoint возвратов (`orders/{id}/refunds`) и форму ответа.

При успехе последняя строка вывода — `WooCommerce REST smoke: all checks passed`.

## Ручная проверка

```bash
curl -k -L -u 'ck_torgnexa_demo_20260829_000000000000000000:cs_torgnexa_demo_20260829_0000000000000000' \
  'https://127.0.0.1:8446/wp-json/wc/v3/products?per_page=10'
```

При необходимости смотрите логи:

```bash
docker compose -f docker-compose.woocommerce-test.yml logs --tail=200 woocommerce
docker compose -f docker-compose.woocommerce-test.yml logs --tail=200 db
```

## Что именно подтверждает этот стенд

Он квалифицирует официальный WooCommerce REST API, Basic Auth по TLS,
товары/цены/управляемые остатки, чтение заказов и запись статуса заказа на
синтетических данных. Общий production worker TORGNEXA сейчас маршрутизирует
только сущность `products`; поэтому этот стенд не превращает цены, остатки,
заказы и возвраты в заявленную generic-синхронизацию без отдельного domain
bridge.

## Остановка и очистка

После проверки удалите контейнеры и тестовую базу:

```bash
docker compose -f docker-compose.woocommerce-test.yml down -v
```

Удаляется только volume `woocommerce-demo-db`; production volumes и исходники
не затрагиваются.
