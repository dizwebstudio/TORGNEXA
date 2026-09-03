# ADR-0130 — печатная форма заявки ПЭК

Status: Accepted

## Context

Официальный API ПЭК разделяет документы печати заказа: `type=simple` выдаёт
этикетки груза, `type=big` — печатную форму заявки, а `type=multiple` — пакет
этикеток. В runtime уже был bounded `logistics.label.read` для одной PDF-
этикетки, но форма заявки оставалась fail-closed.

## Decision

Расширить существующий `LabelRequest` ПЭК явным форматом `request_pdf`. Host
transport вызывает `POST /api/v1/order/print/` на фиксированном официальном
host, передаёт строго один числовой cargo code и `type=big`. Провайдерский
ответ принимается только после ограничения тела, разбора base64 и проверки
`application/pdf`/сигнатуры `%PDF-`. Наружу выходит только content-addressed
opaque `ArtifactRef` с префиксом `pek:print:big:`.

Формат `pdf` сохраняет существующий `type=simple` и свой digest-префикс.
Capability, Core-модель, база данных и секретная модель не меняются; оба
документа используют существующий permission/policy/API путь.

## Consequences

Оператор может выбрать этикетку груза или форму заявки в одном bounded UI-
маршруте. Содержимое PDF, исходный provider response и credentials не
попадают в SDK, события или журналы. Неподдержанный `type=multiple`, любой
неоднозначный сетевой результат и malformed response остаются fail-closed.

## Alternatives considered

Переиспользовать `type=multiple` было отклонено, потому что пакетная печать
имеет другой риск, размер ответа и отдельный контракт qualification.

## Compatibility impact

Изменение обратно совместимо: запросы `format=pdf` не меняются. Клиент должен
явно использовать `format=request_pdf` для формы заявки.

## Migration and data impact

Миграция не требуется: документ передаётся через существующий bounded label
artifact boundary и не создаёт новую доменную запись.

## Security and privacy impact

Cargo code, credentials и PDF bytes остаются внутри host transport; наружу
выходит только проверенный opaque artifact reference.

## Operational impact

Live qualification остаётся release gate для тестового кабинета ПЭК и проверки
реального документа; пакетная печать и операции записи не объявляются.
