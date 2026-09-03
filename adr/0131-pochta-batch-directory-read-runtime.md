# ADR-0131 — bounded чтение партий Почты России

Status: Accepted

## Context

Официальный API «Отправка» Почты России предоставляет `GET /1.0/batch` для
чтения справочника партий. В TORGNEXA отсутствовала безопасная read-only
проекция: оператор не мог увидеть состояние уже существующих партий, а
формирование и передача партий остаются отдельными операциями с более высоким
риском.

## Decision

Добавить capability `logistics.batches.read` и provider-neutral маршрут
`GET /api/v1/logistics/batches`. Host transport вызывает фиксированный
`otpravka-api.pochta.ru/1.0/batch`, передаёт только `mailType`,
`mailCategory`, `size` и `page`, ограниченные runtime. За границу host
выходят только уникальные `batch-name`, безопасный `batch-status`,
неотрицательный `shipment-count` и UTC-время наблюдения.

Сырые provider payloads, состав заказов и credentials не сохраняются и не
публикуются. Формирование партии и hand-off вынесены в отдельные
approval-bound runtime routes Task 184 и Task 185; пакетные записи остаются
fail-closed до отдельной qualification.

## Consequences

Оператор получает bounded справочник партий в API и карточке кабинета Почты
России. Повторное чтение безопасно и не меняет состояние провайдера. Строгая
валидация означает, что неполный, дублирующий или слишком большой ответ
показывается как ошибка, а не превращается в недостоверные данные.

## Alternatives considered

Оставить batch directory только в adapter logs было отклонено: оператору нужна
bounded read-only проекция без доступа к сырому provider payload.

## Compatibility impact

Добавляется обратно совместимый capability, API route и сгенерированная SDK
операция `listLogisticsBatches`; существующие routes и label formats не
меняются.

## Migration and data impact

Миграция не требуется: справочник читается удалённо и не создаёт локальную
копию состава партий или новую доменную таблицу.

## Security and privacy impact

Credentials, raw responses and order composition не пересекают host boundary;
наружу выходят только bounded identifiers, status и UTC observation time.

## Operational impact

Live qualification нужна для проверки актуального тестового кабинета и не
заменяет reconciliation для batch creation или hand-off.
