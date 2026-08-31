# ADR-0168 — отмена Pre-Alert пакетной заявки «Деловых Линий»

Status: Accepted

## Context

«Деловые Линии» документируют отдельную операцию расформирования уже созданной
Pre-Alert пакетной заявки. Она использует числовой `batchRequestID` и не
является отменой отдельного терминального заказа или возвратом груза.

## Decision

Добавить capability `logistics.batches.cancel` и approval-bound маршрут
`POST /api/v1/logistics/batches/cancel/{batch_id}`. Dellin transport вызывает
только официальный `POST /v2/batch_request/cancel.json` после короткой
SecretProvider-scoped login-сессии. Допускается только HTTP/API status 200 и
`data.state=success`; host возвращает точный batch ID, `CANCELLED`,
`cancelled=true` и время наблюдения.

## Security and privacy impact

Маршрут требует authenticated workspace scope, активный logistics account,
включённую write-sensitive capability, matching approval и tenant-scoped
operation receipt. PAT и session ID не покидают transport; в Core не попадает
содержимое пакетной заявки.

## Compatibility impact

Изменение аддитивное: добавляются capability, OpenAPI operation, generated SDK
method, runtime admission и UI control. Отмена отдельных отправлений и
ручные возвраты не меняются.

## Migration and data impact

Миграция не требуется. Используется существующее хранилище operation receipt.

## Operational impact

После успешного ответа провайдера пакетная заявка считается расформированной.
Ошибки, malformed response, нечисловой ID и неоднозначный сетевой исход
закрываются fail-closed; capability можно отключить без изменения остальных
маршрутов доставки.

## Source contract

Официальная [спецификация Pre-Alert](https://dev.dellin.ru/api/ordering/pre-alert/),
раздел «Отмена пакетного заказа».
