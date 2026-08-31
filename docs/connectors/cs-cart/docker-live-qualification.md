# CS-Cart: Docker/live-квалификация

CS-Cart не поставляется в этом репозитории как свободный Docker-образ: для
настоящего магазина нужен лицензированный дистрибутив. Официальное окружение
[cscart/development-docker](https://github.com/cscart/development-docker)
поднимает PHP/nginx/MySQL, но ожидает файлы CS-Cart в `app/www`; эмулятор API
не может заменить проверку настоящего магазина.

## Подготовка Docker-магазина

1. Получите тестовый CS-Cart package у вендора. Не используйте production-базу
   и production API key.
2. Запустите официальное окружение:

   ```bash
   git clone https://github.com/cscart/development-docker.git /tmp/cscart-development-docker
   cd /tmp/cscart-development-docker
   mkdir -p app/www
   # Распакуйте выданный вендором архив CS-Cart в app/www.
   cp config/nginx/app.conf.example config/nginx/app.conf
   make -f Makefile run
   ```

   После установки включите администратору `API access` в
   `Customers → Administrators`. Это соответствует
   [официальному REST API guide](https://docs.cs-cart.com/latest/developer_guide/api/index.html).
   API key храните только в secret manager или shell окружении.

3. Проверьте доступность магазина с машины, где запускается smoke. Для
   удалённого endpoint нужен HTTPS с проверяемым сертификатом; HTTP разрешён
   только на loopback-адресе локального Docker.

## Credentialed smoke

Задайте тестовые значения (не добавляйте их в `.env` репозитория):

```bash
export CS_CART_BASE_URL=http://127.0.0.1
export CS_CART_EMAIL=admin@example.com
export CS_CART_API_KEY='issued-by-the-test-store'
export CS_CART_ALLOW_HTTP=1       # только для локального Docker
# export CS_CART_INSECURE_TLS=1   # только для локального self-signed TLS
scripts/cscart-smoke.sh
```

Для staging endpoint:

```bash
CS_CART_BASE_URL=https://cscart-staging.example.test \
CS_CART_EMAIL=admin@example.com \
CS_CART_API_KEY="$CS_CART_TEST_API_KEY" \
scripts/cscart-smoke.sh
```

Скрипт использует API 2.0 и Basic Auth (`email:API key`), проверяет `401` без
credentials, bounded catalog/price/inventory/order read, создание синтетического товара, поиск по
`pcode`, чтение по ID, обновление и read-after-write. Созданный тестовый товар
удаляется в `trap`; `CS_CART_KEEP_PRODUCT=1` оставляет его для ручного осмотра.
Ключ и ответы магазина не выводятся и не сохраняются как evidence. При
отсутствии endpoint/credentials скрипт завершается кодом `2` (live-проверка не
запущена).

## Критерий закрытия

Статус можно сменить с `BLOCKED` только после запуска на настоящем магазине и
сохранения обезличенной записи с датой, версией CS-Cart, URL без credentials и
перечнем операций. Ожидаемая строка успешного запуска:

```text
CS-Cart REST API 2.0 live smoke: all checks passed
```

До этого момента CS-Cart остаётся repository-qualified: SDK 13/13, products,
base price, inventory и order reads, а также стандартная смена статуса заказа
в runtime; live/Docker-совместимость не заявляется.
