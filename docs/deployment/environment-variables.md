# Переменные окружения Community-развёртывания

Этот документ описывает корневой файл `.env`, который используется командами
`make community-*` и `docker compose --env-file .env ...`.

Переменные `DEPLOY_*` GitHub Environment относятся только к SSH-транспорту
защищённого production workflow и не заменяют этот `.env`; их настройка и
требования к production-хосту описаны в
[руководстве SSH-деплоя](production-ssh-deploy.md).

Заполненный `.env` содержит пароли и ключ шифрования. Он исключён из Git и
должен иметь права `0600`. Не публикуйте его, не прикладывайте к обращениям и
не копируйте значения в логи.

## Рекомендуемый способ заполнения

Не копируйте `.env.example` как готовый `.env`: секреты в шаблоне специально
оставлены пустыми. Для новой установки выполните:

~~~bash
make community-init
make community-up
~~~

`make community-init` вызывает `scripts/init-community-env.sh`, создаёт
случайные пароли и ключи, добавляет безопасные локальные значения и выставляет
права `0600`. `make community-up` делает то же автоматически, если `.env`
отсутствует.

После генерации обычно требуется изменить только:

1. занятые порты;
2. `TORGNEXA_LOG_LEVEL`, если нужен диагностический журнал;
3. allowlist внешних OIDC issuer;
4. параметры SMTP/chat, если нужны внешние уведомления;
5. ClamAV, прежде чем включать обработку загрузок.

Проверка конфигурации без запуска сервисов:

~~~bash
docker compose --env-file .env config --quiet
make community-check
~~~

Запуск и проверка состояния:

~~~bash
make community-up
make community-status
~~~

## Правила синтаксиса

- одна строка имеет вид `ИМЯ=значение`;
- пробелы вокруг `=` не добавляются;
- логические значения: `true` или `false`;
- длительности записываются в Go-формате: `500ms`, `30s`, `5m`, `1h`;
- CSV-списки разделяются запятыми без пустых элементов;
- порты — целые числа от 1 до 65535;
- для сгенерированных шестнадцатеричных секретов кавычки не требуются.

## Версия и журналирование

| Переменная | По умолчанию | Как заполнять |
|---|---:|---|
| `TORGNEXA_VERSION` | `0.1.0-dev` | Версия образа и метка миграций. Для локальной разработки оставьте значение; для релиза используйте фактическую SemVer-версию. |
| `TORGNEXA_LOG_LEVEL` | `info` | `debug`, `info`, `warn` или `error`. |

## Публичный адрес frontend и документации

| Переменная | По умолчанию | Как заполнять |
|---|---:|---|
| `TORGNEXA_PUBLIC_URL` | не задано в production | Полный публичный HTTPS-адрес frontend без завершающего `/`, например `https://app.example.ru`. Production overlay передаёт его в frontend-сборку; значение встраивается в canonical, Open Graph, JSON-LD и `sitemap.xml`. |

Для локальной сборки документации переменная необязательна: используется
`http://127.0.0.1:5173`. Для production она обязательна — Compose остановит
рендеринг конфигурации, если переменная не задана. После изменения адреса
пересоберите frontend, чтобы обновились статический HTML, `robots.txt` и
`sitemap.xml`.

`TORGNEXA_ENV`, формат журнала, HTTP-адреса и security-edge параметры для
Community Compose задаются непосредственно в `docker-compose.yml`. Изменять их
через `.env` для публичной production-топологии недостаточно.

## Обязательные секреты

Все значения в этом разделе создаются автоматически. Вручную заполнять их для
обычной Community-установки не нужно.

| Переменная | Назначение и формат |
|---|---|
| `TORGNEXA_SECRETS_MASTER_KEY` | Base64 от ровно 32 случайных байт. Шифрует секреты интеграций. Потеря ключа делает ранее сохранённые секреты нечитаемыми. |
| `POSTGRES_PASSWORD` | Пароль владельца основной PostgreSQL-базы; используется миграциями и bootstrap-задачами. |
| `TORGNEXA_APP_DB_PASSWORD` | Отдельный пароль ограниченной роли `torgnexa_app`, под которой работают приложения. |
| `KEYCLOAK_DB_PASSWORD` | Пароль отдельной PostgreSQL-роли и базы Keycloak. |
| `KEYCLOAK_ADMIN_USERNAME` | Имя локального bootstrap-администратора Keycloak; стандартно `admin`. |
| `KEYCLOAK_ADMIN_PASSWORD` | Случайный пароль bootstrap-администратора Keycloak. |
| `GARAGE_RPC_SECRET` | 64 шестнадцатеричных символа для внутреннего RPC Garage. |
| `GARAGE_ADMIN_TOKEN` | Административный токен Garage. |
| `GARAGE_METRICS_TOKEN` | Токен метрик Garage. |
| `S3_ACCESS_KEY` | Идентификатор доступа к S3-совместимому Garage; генератор использует префикс `GK`. |
| `S3_SECRET_KEY` | Секрет соответствующего S3-ключа. |
| `CLICKHOUSE_PASSWORD` | Пароль пользователя аналитической базы ClickHouse. |

Никогда не заменяйте эти значения по одному в работающей установке. PostgreSQL,
Keycloak, ClickHouse и Garage сохраняют credentials в persistent volumes.
Изменение только `.env` приведёт к отказу подключения.

## Хранилище и аналитика

| Переменная | По умолчанию | Ограничения |
|---|---:|---|
| `S3_BUCKET` | `torgnexa` | Строчные буквы, цифры, точки и дефисы. Bucket создаётся при первом запуске. |
| `TORGNEXA_S3_REQUEST_TIMEOUT` | `30s` | От `1s` до `2m`. |
| `CLICKHOUSE_USERNAME` | `torgnexa` | Пользователь ClickHouse. Не меняйте после создания volume без согласованной ротации. |
| `TORGNEXA_CLICKHOUSE_QUERY_TIMEOUT` | `5s` | От `100ms` до `30s`. |

Внутренние адреса `S3_ENDPOINT` и `CLICKHOUSE_DSN` задаёт Compose. Порты ниже
нужны только для доступа с хоста и не меняют адреса между контейнерами.

## Порты хоста

Все публикации Community Compose привязаны к `127.0.0.1`.

| Переменная | По умолчанию | Сервис |
|---|---:|---|
| `POSTGRES_PORT` | `5432` | PostgreSQL |
| `KAFKA_PORT` | `9092` | Kafka |
| `VALKEY_PORT` | `6379` | Valkey |
| `CLICKHOUSE_HTTP_PORT` | `8123` | ClickHouse HTTP |
| `CLICKHOUSE_NATIVE_PORT` | `9000` | ClickHouse native protocol |
| `S3_PORT` | `9002` | Garage S3 API |
| `KEYCLOAK_PORT` | `8081` | Keycloak |
| `TORGNEXA_API_PORT` | `8080` | TORGNEXA REST API |
| `TORGNEXA_MCP_PORT` | `8090` | MCP transport |
| `TORGNEXA_FRONTEND_PORT` | `5173` | Web-интерфейс |

Если порт занят, меняйте только хостовый порт:

~~~dotenv
TORGNEXA_FRONTEND_PORT=5174
POSTGRES_PORT=55432
~~~

После изменения frontend или Keycloak порта потребуется согласованно обновить
OIDC redirect URI, Web Origins и разрешённые origins. Для стандартной локальной
установки лучше оставить `5173` и `8081`.

## MTU внутренней Docker-сети

| Переменная | По умолчанию | Как заполнять |
|---|---:|---|
| `TORGNEXA_DOCKER_NETWORK_MTU` | `1376` | Целое значение MTU bridge-сети. Оно должно быть не больше MTU интерфейса, через который контейнеры выходят во внешние API. |

Заниженный MTU немного увеличивает сетевые накладные расходы, но безопасен.
Завышенный MTU на VPN/туннельном хосте может проявляться как таймаут TLS
handshake только из контейнеров, хотя тот же URL открывается с хоста. Узнать MTU
egress-интерфейса можно командой `ip link show <интерфейс>`. После изменения
значения требуется пересоздать Docker-сеть и контейнеры:

~~~bash
docker compose --env-file .env down
docker compose --env-file .env up -d
~~~

Volumes при этом не удаляются. Не используйте `down -v`, если не хотите удалить
данные PostgreSQL, ClickHouse, Garage и Valkey.

## Пул PostgreSQL

| Переменная | По умолчанию | Допустимый диапазон |
|---|---:|---|
| `TORGNEXA_DB_MAX_OPEN_CONNS` | `20` | `1`–`1000` |
| `TORGNEXA_DB_MAX_IDLE_CONNS` | `10` | `0`–`MAX_OPEN_CONNS` |
| `TORGNEXA_DB_CONN_MAX_LIFETIME` | `30m` | `1m`–`24h` |
| `TORGNEXA_DB_CONN_MAX_IDLE_TIME` | `5m` | `1s`–`1h` |
| `TORGNEXA_DB_CONNECT_TIMEOUT` | `5s` | `100ms`–`1m` |

Увеличение пула умножается на количество процессов и экземпляров. Учитывайте
общий лимит подключений PostgreSQL.

## OIDC

| Переменная | По умолчанию | Как заполнять |
|---|---:|---|
| `TORGNEXA_OIDC_MANAGED_ISSUER_HOSTS` | пусто | CSV-список разрешённых host внешних issuer без схемы, пути и пробелов. Пустое значение означает default deny. |

Пример:

~~~dotenv
TORGNEXA_OIDC_MANAGED_ISSUER_HOSTS=login.company.ru,id.company.ru
~~~

Allowlist не заменяет discovery-проверку, mapping ролей и явную активацию
провайдера в интерфейсе.

## Worker и Kafka

| Переменная | По умолчанию | Ограничения |
|---|---:|---|
| `TORGNEXA_KAFKA_TOPIC_PARTITIONS` | `1` | Положительное целое. Число partitions для создаваемых Compose topics. |
| `TORGNEXA_KAFKA_TOPIC_REPLICATION_FACTOR` | `1` | Положительное целое; не больше числа доступных Kafka brokers. Для одновузлового Community — `1`. |
| `TORGNEXA_KAFKA_CONSUMER_GROUP` | `torgnexa.webhooks.v1` | Непустое имя до 128 символов. Экземпляры одного worker-кластера используют одну группу. |
| `TORGNEXA_WORKER_POLL_INTERVAL` | `500ms` | `50ms`–`30s` |
| `TORGNEXA_WORKER_DISPATCH_BATCH` | `32` | `1`–`1000` |
| `TORGNEXA_WORKER_LEASE` | `90s` | `10s`–`10m`; должно превышать время обработки пачки. |
| `TORGNEXA_WORKER_RECONCILIATION_ENABLED` | `true` | Включает периодическую сверку и обработку расхождений. |
| `TORGNEXA_WORKER_UPLOADS_ENABLED` | `true` | В Community Compose включено для загрузки изображений; worker выпускает только файлы, прошедшие ClamAV и S3-пайплайн. |

Community Compose использует внутренний broker `kafka:29092`. Одноразовый
сервис `kafka-init` идемпотентно создаёт канонический набор base topics и их
`.retry`/`.dlq` варианты до запуска API, worker, scheduler и MCP. При прямом запуске worker вне Community Compose список
`TORGNEXA_KAFKA_TOPICS` должен включать как минимум
`commerce.inventory.events.v1` и `commerce.pricing.events.v1`, иначе отдельный
маршрут `commerce-sync` не получит изменения остатков и цен.

## Проверка загрузок ClamAV

| Переменная | По умолчанию | Назначение |
|---|---:|---|
| `TORGNEXA_CLAMAV_NETWORK` | `tcp` | Сетевой тип подключения. |
| `TORGNEXA_CLAMAV_ADDRESS` | `clamav:3310` | Адрес `host:port` контейнера ClamAV. Для внешнего worker укажите адрес доступного scanner. |
| `TORGNEXA_CLAMAV_ENGINE_VERSION` | `runtime` | Метка версии движка для аудита. |
| `TORGNEXA_CLAMAV_SIGNATURE_VERSION` | `runtime` | Метка базы сигнатур. |
| `TORGNEXA_CLAMAV_TIMEOUT` | `30s` | От `1s` до `2m`. |

Community Compose уже запускает официальный контейнер ClamAV и подключает его к
worker по адресу `clamav:3310`. Недоступный сканер не обходится: загрузки
остаются в карантине. Для отдельного worker подключите собственный ClamAV перед
включением `TORGNEXA_WORKER_UPLOADS_ENABLED=true`.

## Внешние уведомления

Пустой SMTP или chat endpoint отключает соответствующий транспорт; in-app
уведомления продолжают работать.

| Переменная | Пример | Назначение |
|---|---|---|
| `NOTIFICATION_SMTP_ADDRESS` | `smtp.company.ru:587` | TCP-адрес SMTP. |
| `NOTIFICATION_SMTP_FROM` | `torgnexa@company.ru` | Отправитель. Для email нужны одновременно address и from. |
| `NOTIFICATION_SMTP_USERNAME` | `torgnexa` | Необязательное имя SMTP-пользователя. |
| `NOTIFICATION_SMTP_PASSWORD` | секрет | Пароль SMTP. |
| `NOTIFICATION_SMTP_SERVER_NAME` | `smtp.company.ru` | Имя сервера для TLS-проверки сертификата. |
| `NOTIFICATION_SMTP_IMPLICIT_TLS` | `false` | `true` только для implicit TLS; для STARTTLS оставьте `false`. |
| `NOTIFICATION_CHAT_ENDPOINT` | `https://bot-gateway.company.ru/...` | HTTPS endpoint корпоративного bot gateway. |
| `NOTIFICATION_DELIVERY_TIMEOUT` | `10s` | От `1s` до `1m`. |

Пример SMTP:

~~~dotenv
NOTIFICATION_SMTP_ADDRESS=smtp.company.ru:587
NOTIFICATION_SMTP_FROM=torgnexa@company.ru
NOTIFICATION_SMTP_USERNAME=torgnexa
NOTIFICATION_SMTP_PASSWORD=replace-with-secret
NOTIFICATION_SMTP_SERVER_NAME=smtp.company.ru
NOTIFICATION_SMTP_IMPLICIT_TLS=false
NOTIFICATION_DELIVERY_TIMEOUT=10s
~~~

## Что делать с уже существующим `.env`

Если установка работает и volumes сохранены, не запускайте генератор с
`--force` и не заменяйте секреты. Добавляйте только новые несекретные
переменные из `.env.example`, сохраняя старые credentials.

Если `.env` был потерян, но volumes остались, создание нового файла не
восстановит доступ. Нужен защищённый backup исходного файла или процедура
согласованной ротации credentials внутри каждого хранилища.

Если это новая установка без данных и `.env` создан копированием шаблона с
пустыми секретами, сохраните несекретные настройки отдельно, уберите
нерабочий файл из корня и выполните `make community-init`. Не делайте этого
для работающей установки с persistent volumes.

## Production

Корневой `.env` и Compose-файл предназначены для single-host Community
контура. В production используйте внешний secret manager, Docker/Kubernetes
secrets, TLS edge, внешние базы и отдельные процедуры ротации.
