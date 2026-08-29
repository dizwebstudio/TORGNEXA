# PrestaShop: проверка Webservice API в Docker Compose

В репозитории есть изолированный стенд на официальном образе
`prestashop/prestashop:8.1-apache` и MariaDB 11.4. Он устанавливает свежий
магазин, включает штатный Webservice API, создаёт отдельный API key и загружает
только синтетические товары. Админские и production-секреты в стенд не
попадают.

## Требования

- Docker Engine с Compose v2;
- свободный TCP-порт `8097` (его можно заменить через
  `PRESTASHOP_HTTP_PORT`);
- доступ Docker daemon к Docker Hub и Debian mirror на время сборки.

Версия и SHA-256 digest базового образа зафиксированы в
`docker-compose.prestashop-test.yml`, поэтому сборка воспроизводима. Внешний API не
публикуйте в интернет: стенд предназначен только для локальной квалификации.

## Запуск

Из корня репозитория:

```bash
docker compose -f docker-compose.prestashop-test.yml up -d --build
docker compose -f docker-compose.prestashop-test.yml ps
```

Первый запуск выполняет штатную CLI-установку PrestaShop и затем seed:

- включает настройку `PS_WEBSERVICE`;
- создаёт ключ `0123456789abcdef0123456789abcdef` с правами только на
  `products`, `combinations`, `stock_availables`, `orders`, `order_details` и
  `order_histories`;
- добавляет `TORGNEXA-PS-COFFEE` (остаток 24, цена `1499.90` EUR) и
  `TORGNEXA-PS-TEA` (остаток 8, цена `799.00` EUR).

Локальные параметры стенда:

| Параметр | Значение |
| --- | --- |
| витрина | `http://127.0.0.1:8097` |
| Webservice base URL | `http://127.0.0.1:8097/api` |
| API key / Basic username | `0123456789abcdef0123456789abcdef` |
| Basic password | пустой (`key:`) |
| валюта демо-магазина | `EUR` |
| MariaDB database | `prestashop` |
| MariaDB user/password | `prestashop` / `prestashop-demo` |

Ключ и пароли синтетические. Ключ передаётся только в Basic Auth и не должен
появляться в URL, логах или production SecretProvider.

## Полный smoke-тест

После состояния `healthy` выполните:

```bash
scripts/prestashop-smoke.sh
```

Скрипт проверяет:

1. отказ без Basic Auth (`401`) и успешный доступ по API key;
2. список товаров, оба synthetic reference и чтение одного товара;
3. XML `PATCH /api/products/{id}` с последующей проверкой цены;
4. поиск `StockAvailable`, XML `PATCH /api/stock_availables/{id}` и проверку
   нового количества;
5. доступность официального ресурса `/api/orders`.

PrestaShop 8.1 возвращает JSON-объект ресурса даже для URL с `{id}` в
коллекционной обёртке (`{"products":[...]}`, `{"orders":[...]}`). Коннектор
принимает этот официальный формат, а также прежнюю singular-форму для
совместимости с тестовым эмулятором. JSON используется только для чтений;
официальный Webservice input для изменений остаётся XML `PATCH`/`POST`.

При успехе последняя строка вывода — `PrestaShop Webservice smoke: all checks passed`.

## Ручная проверка и диагностика

```bash
curl --globoff -u '0123456789abcdef0123456789abcdef:' \
  'http://127.0.0.1:8097/api/products?output_format=JSON&display=[id,reference,name,price]&limit=10'
docker compose -f docker-compose.prestashop-test.yml logs --tail=200 prestashop
docker compose -f docker-compose.prestashop-test.yml logs --tail=200 db
```

Если API отвечает `503` с сообщением о выключенном Webservice, дождитесь
завершения установки и перезапустите контейнер: init-script включает
`PS_WEBSERVICE` и предварительно компилирует Webservice-контейнер, чтобы
параллельные health-check и smoke-запросы не столкнулись на cache-файле.

## Что подтверждает стенд

Он квалифицирует именно официальный PrestaShop Webservice API: Basic Auth с
ключом, JSON catalog reads, комбинационные/StockAvailable ресурсы и XML
desired-state writes для цены и остатка. Production built-in runtime TORGNEXA
маршрутизирует эти две записи отдельным outbound worker-потоком; один только
Webservice smoke не создаёт sync policy, connector account или `offer`
mapping. Для полной проверки цепочки API → outbox → Kafka → worker → PrestaShop
создайте outbound-политики `prices` и `inventory`, включите соответствующие
capabilities аккаунта и используйте тот же synthetic offer mapping.

## Остановка и очистка

После проверки удалите контейнеры и synthetic volume:

```bash
docker compose -f docker-compose.prestashop-test.yml down -v
```

Удаляется только `prestashop-demo-db`; исходники и production volumes не
затрагиваются.
