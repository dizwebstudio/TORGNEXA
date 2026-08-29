# TORGNEXA

[English version](README.md)

TORGNEXA — это open-source API-first платформа для самостоятельного развёртывания,
которая объединяет коммерческие и дистрибуционные операции: маркетплейсы,
интернет-магазины, социальные каналы, ERP, PIM/MDM, склад, заказы, финансы,
комплаенс и автоматизацию.

## Что входит в проект

- провайдеронейтральное ядро торговли и дистрибуции;
- коннекторы маркетплейсов, интернет-магазинов, классифайдов, социальных
  каналов, ERP, платежей, логистики и регулируемых систем;
- каталог/PIM, цены, остатки, заказы, возвраты, закупки, WMS, fulfillment и
  расчёты;
- REST API, OpenAPI-контракты, подписанные webhooks, MCP/OpenClaw и границы
  интеграции с n8n;
- tenant-scoped governance, согласования, аудит/lineage, secrets, privacy,
  безопасность загрузок, IAM и SIEM;
- React/TypeScript frontend, Go-сервисы, миграции, Docker Compose и CI-инструменты
  валидации.

## Структура репозитория

- `docs/` — архитектурная и предметная документация;
- `adr/` — архитектурные решения;
- `contracts/` — OpenAPI, события, плагины, webhooks и JSON Schema;
- `connectors/` — реализации провайдеров через SDK, сгруппированные по категориям;
- `internal/` — пакеты core, platform и application;
- `frontend/` — приложение React/TypeScript/Vite;
- `tasks/` — задачи реализации и планы выполнения;
- `scripts/`, `deploy/`, `docker-compose*.yml` — инструменты разработки и
  развёртывания.

## Архитектура

TORGNEXA начинается как модульный монолит на Go. PostgreSQL является
операционной системой истины; Kafka — надёжной событийной платформой;
ClickHouse хранит аналитику и историю; Valkey используется только для cache,
lock и rate-limit состояния; S3-совместимое хранилище содержит медиа и
evidence-артефакты.

Core не ветвится по именам провайдеров. Коннекторы реализуют SDK-порты и
декларации capabilities, а host-side runtime adapters отвечают за сетевой
доступ, secret callbacks, policy checks и ограниченные повторы. Архитектура v1
зафиксирована в [`docs/54-architecture-freeze-v1.md`](docs/54-architecture-freeze-v1.md).

## Быстрый запуск Community

Требования: Docker с Compose v2 и локальная копия репозитория.

```bash
make community-init
make community-up
make community-status
```

Community-стек запускается локально на loopback и включает PostgreSQL, Kafka,
Valkey, ClickHouse, S3-совместимое хранилище, ClamAV, Keycloak, API, worker,
scheduler, MCP-сервис и frontend. Для локального демо используется
синтетическая учётная запись Keycloak `demo` / `demo-local-only`.

Для полного браузерного smoke-теста после запуска стека выполните:

```bash
make community-e2e
```

Описание конфигурации и безопасной ротации секретов находится в
[`docs/deployment/environment-variables.md`](docs/deployment/environment-variables.md).
Не используйте `.env.example` как production-конфигурацию. Инструкции по
развёртыванию находятся в
[`docs/deployment/093-community-docker-deployment.md`](docs/deployment/093-community-docker-deployment.md)
и [`docs/deployment/production-ssh-deploy.md`](docs/deployment/production-ssh-deploy.md).

## Валидация и evidence

В отслеживаемом Git-файле [`VALIDATION_REPORT.md`](VALIDATION_REPORT.md)
хранится краткое резюме валидации репозитория. Подробные runtime- и
release-доказательства создаются в `qualification/evidence/` или сохраняются
как CI artifacts; эти изменчивые результаты намеренно не коммитятся вместе с
исходным кодом.

## Разработка

Перед изменениями прочитайте [`docs/00-product-scope.md`](docs/00-product-scope.md),
[`docs/01-architecture.md`](docs/01-architecture.md),
[`docs/03-module-boundaries.md`](docs/03-module-boundaries.md) и соответствующую
карточку задачи. Изменения публичного API выполняются contract-first через
`contracts/openapi/`; mutating operations должны сохранять tenant scope,
idempotency, auditability и security policy.

Стандартные проверки репозитория:

```bash
gofmt -w <changed-go-files>
go test ./...
go vet ./...
./scripts/check-contracts.sh
```

## Лицензия

TORGNEXA распространяется по лицензии Apache License 2.0. См. [`LICENSE`](LICENSE).
