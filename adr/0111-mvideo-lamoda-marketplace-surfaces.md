# ADR 0111: M.Видео и Lamoda как health-check поверхности маркетплейсов

Status: Accepted

## Context

Пользователю нужны карточки М.Видео и Lamoda в каталоге маркетплейсов, но
наличие SDK-манифеста само по себе не подтверждает доступность актуального
partner/Seller API, scopes или worker-маршрута. Публикация неподтверждённых
операций создала бы ложное обещание production-интеграции.

## Decision

Добавить оба провайдера под `connectors/marketplaces/<id>` и показать их на
поверхности «Маркетплейсы» как `separate_surface` с флагом `health_only`.
Кабинет, API key и операторский HTTPS `probe_url` проходят существующую
tenant-scoped цепочку и bounded host-mediated health probe. Операционные
capabilities и worker sync остаются пустыми до отдельной квалификации.

## Consequences

Карточки доступны для поиска, сохранения credentials и проверки соединения.
Интерфейс не позволяет включить товары, цены, остатки, заказы или webhooks,
пока не появятся fixtures, тестовый кабинет, актуальный контракт API и
идемпотентный worker bridge.

## Compatibility impact

Изменение аддитивно: Connector SDK v1, публичный OpenAPI, события и схемы БД
не меняются. Новое значение runtime surface используется только в
сгенерированных проекциях каталога.

## Migration and data impact

Миграция не требуется. Используются существующие connector accounts,
SecretProvider и история health-check; plaintext credentials не попадают в
конфигурацию сборки или логи.

## Security and privacy impact

API key раскрывается только callback-обработчику SecretProvider. Probe требует
HTTPS, запрещает private/loopback hosts, ограничивает таймаут и тело ответа и
нормализует ошибки без provider payload или секрета.

## Operational impact

Оператор должен получить актуальный endpoint и credentials в кабинете
партнёра. Зелёная проверка подтверждает только доступность конкретного
кабинета; domain-синхронизация и записи остаются закрыты.

## Alternatives considered

Оставить провайдеры невидимыми или в `planned` означало бы скрыть безопасный
контур проверки. Объявить их полностью готовыми означало бы выдать
неподтверждённый API и нарушить runtime-truthful каталог. Выбран health-only
контур с отдельной будущей квалификацией.
