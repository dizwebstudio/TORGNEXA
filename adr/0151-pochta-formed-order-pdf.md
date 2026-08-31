# ADR-0151 — форма сформированного заказа Почты России

Status: Accepted

## Context

Официальная спецификация API «Отправка» содержит генерацию печатных форм для
заказа. Runtime уже умел получать PDF обычного заказа из backlog, возвратную
этикетку и Ф103 партии, но форма заказа после формирования партии оставалась
fail-closed.

## Decision

Расширить существующий `LabelRequest` Почты России явным форматом
`formed_order_pdf`. Host transport вызывает фиксированный официальный маршрут
`GET /1.0/forms/{order-id}/forms`, принимает один числовой ID заказа и
передаёт `print-type=PAPER` вместе с текущей UTC-датой `sending-date`.
Принимаются только HTTP 2xx, `application/pdf` и сигнатура `%PDF-`. Наружу
выходит только content-addressed opaque `ArtifactRef` с префиксом
`pochta-russia:form:formed-order:`.

Форматы `pdf`, `return_pdf` и `batch_f103_pdf` не меняются. Capability,
Core-модель, база данных и секретная модель не меняются: используется
существующий tenant-scoped permission/API путь чтения PDF-документа.

## Security and privacy impact

Токен и ключ пользователя остаются callback-scoped в SecretProvider. PDF,
состав заказа и сырой ответ не попадают в SDK, события, audit или логи.
Числовой order ID проверяется до provider egress, а caller не управляет
host или URL провайдера.

## Compatibility impact

Изменение обратно совместимо: существующие форматы и ответы не меняются.
Клиент должен явно передавать `format=formed_order_pdf`; публичный endpoint
остаётся прежним, меняется только согласованный формат запроса и runtime
admission. OpenAPI-значение добавляется аддитивно.

## Operational impact

Форма строится для одного уже сформированного заказа. Ошибка провайдера,
timeout, пустой ответ или не-PDF остаются ошибками и не превращаются в
фиктивный документ. Live qualification должна использовать заказ из
тестовой партии, а повтор чтения не должен обходить существующие permission,
egress и audit boundaries.

## Migration and data impact

Миграция не требуется. Используется существующая граница label operation и
lineage; сохраняется только непрозрачная ссылка на проверенный PDF.

## Alternatives considered

Оставить форму после партии fail-closed отвергнуто: официальный маршрут и
строгий read-only контракт известны, а существующий transport уже проверяет
PDF. Создавать отдельный Core endpoint отвергнуто: результат имеет тот же
provider-neutral artifact contract и должен проходить те же security/egress
границы.

## Consequences

Оператор может получить форму сформированного заказа из карточки заказа рядом
с формой backlog, возвратной этикеткой и Ф103 партии. Формирование партий,
изменение заказа и прочие неподтверждённые документы не изменяются.
