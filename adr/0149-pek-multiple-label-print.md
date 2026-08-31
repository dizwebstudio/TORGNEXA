# ADR-0149 — пакетная печать этикеток заявки ПЭК

Status: Accepted

## Context

Официальный API ПЭК разделяет документы печати заказа: `type=simple` выдаёт
этикетку одного груза, `type=big` — печатную форму заявки, а `type=multiple` —
пакет этикеток всех грузов заявки. Runtime уже проверял base64/PDF для первых
двух форматов, но пакетная печать оставалась fail-closed.

## Decision

Расширить существующий `LabelRequest` ПЭК явным форматом `multiple_pdf`.
Host transport вызывает `POST /api/v1/order/print/` на фиксированном
официальном host, передаёт один числовой `cargoIndex` и `type=multiple`.
Ответ принимается только после bounded-разбора base64 и проверки сигнатуры
`%PDF-`; наружу выходит только content-addressed opaque `ArtifactRef` с
префиксом `pek:print:multiple:`.

Форматы `pdf` и `request_pdf` сохраняют маршруты `simple` и `big`. Capability,
Core-модель, база данных и секретная модель не меняются: операция использует
существующий permission/API путь чтения этикетки.

## Security and privacy impact

Basic credentials остаются callback-scoped в SecretProvider. В SDK, события и
audit не попадают provider response или PDF body. Код груза должен быть
числовым и bounded; caller-controlled URL и provider-specific payload за
пределы host transport не выходят.

## Compatibility impact

Изменение обратно совместимо: существующие форматы не меняются. Клиент должен
явно передавать `format=multiple_pdf`. Пакет строится по одной заявке через
один cargo code; точное наличие и состав грузов определяет ПЭК. Live
qualification остаётся отдельным release gate для тестового кабинета.

## Operational impact

Пакет печатается по одной заявке, которую ПЭК определяет по переданному
`cargoIndex`. Если заявка ещё не принята или провайдер возвращает ошибку,
операция не создаёт частичный или фиктивный документ.

## Migration and data impact

Миграция не требуется. Используется существующий bounded label operation и
его audit/lineage boundary; сохраняется только непрозрачная ссылка на
проверенный PDF. Provider credentials и бинарное содержимое не записываются.

## Alternatives considered

Сохранить пакетную печать fail-closed отвергнуто: официальный тип операции и
bounded payload уже подтверждены, а существующий label route обеспечивает
общие permission, egress и PDF-validation границы. Создавать новый endpoint
отвергнуто: это не новая доменная операция, а явный provider-specific формат
существующего чтения документа.

## Consequences

Оператор может получить все этикетки заявки через тот же раздел «Доставка»,
не обходя approval, capability и tenant-scoped account gates. Отмена
сформированного груза, адресная доставка, webhooks и иные неподтверждённые
операции ПЭК остаются закрыты.
