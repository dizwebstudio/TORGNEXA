# ADR-0129 — возвратная этикетка Почты России

Status: Accepted

## Context

Коннектор Почты России уже умел получать PDF-печатную форму заказа до
формирования партии, но нейтральный `logistics.label.read` не покрывал
документ «возвратный ярлык на одной печатной странице». Официальная
спецификация «Отправка» выделяет этот документ отдельно от обычных форм.

## Decision

Расширить существующий `LabelRequest` провайдера Почта России форматом
`return_pdf`. Для него host transport принимает только domestic/S10 RPO
barcode и вызывает `GET /1.0/forms/{rpo}/easy-return-pdf` на
`otpravka-api.pochta.ru` с фиксированным `print-type=PAPER`. Ответ допускается
только при HTTP 2xx, media type
`application/pdf` и сигнатуре `%PDF-`. В SDK выходит только SHA-256-based
opaque `ArtifactRef` с префиксом `pochta-russia:form:return:`.

Обычный формат `pdf` сохраняет backlog-маршрут, бумажный тип печати и текущую
дату. Никакие новые секреты, поля Core или capability не добавляются:
`logistics.label.read` уже является допущенной операцией.

## Consequences

Возвратный ярлык доступен через тот же permission/policy/API путь, что и
другая PDF-этикетка. Бинарное содержимое, токены и исходный ответ провайдера
не сохраняются в SDK или событиях. Ошибки маршрута, не-PDF и неверный RPO
fail-closed.

## Compatibility and operational impact

Изменение обратно совместимо: существующий `format=pdf` не меняется. Клиент
должен явно передавать `format=return_pdf` для возвратной этикетки. Live
qualification остаётся отдельным release gate для тестового кабинета Почты
России и проверки доступности формы на реальном RPO.
