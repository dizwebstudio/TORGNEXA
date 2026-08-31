# ADR-0167 — изменение даты передачи партии Почты России

Status: Accepted

## Context

API «Отправка» имеет отдельный endpoint для изменения дня передачи партии в
почтовое отделение. Успешный ответ может быть пустым, поэтому transport не
может требовать универсальную JSON-схему или передавать raw provider payload
в Core.

## Decision

Добавить capability `logistics.batches.sending_date.write` и
approval-bound host route `POST /api/v1/logistics/batches/sending-date/{batch_id}`.
Runtime вызывает только официальный
`POST /1.0/batch/{batch-name}/sending/YYYY/MM/DD` без body/query, проверяет
числовое имя партии и календарную дату, принимает пустой `2xx` либо JSON без
`error-code` и возвращает нейтральную проекцию с точным batch ID, ISO-датой,
`UPDATED`, `updated=true` и временем наблюдения.

## Security and privacy impact

Маршрут требует authenticated workspace scope, активный logistics account,
включённую capability и заранее одобренный write-sensitive запрос. Секреты
читаются только через SecretProvider. Tenant-scoped operation receipt
предотвращает повторный внешний вызов; pending/неоднозначный исход не
повторяется автоматически. Raw response не сохраняется.

## Compatibility impact

Изменение аддитивное: добавляются capability, OpenAPI operation, generated
SDK method и UI control. Существующие формирование, check-in, archive и
restore маршруты не меняются.

## Migration and data impact

Миграция не требуется. Используется существующее хранилище operation receipt.

## Operational impact

Оператор может изменить дату передачи сформированной партии из карточки
кабинета Почты России. Ошибки provider, malformed response и расхождение
идентификатора закрываются fail-closed.

## Alternatives considered

Передавать дату через существующий `logistics.batches.create` отвергнуто:
создание партии и изменение даты уже существующей партии — разные provider
операции. Требовать непустой JSON-ответ отвергнуто из-за официального
пустого успешного ответа.

## Consequences

Операция доступна через общий connector runtime только после явного
включения capability и approval; подтверждённые поля остаются стабильными
для Core и SDK.
