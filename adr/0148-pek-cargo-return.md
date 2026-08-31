# ADR-0148 — Возврат принятого груза ПЭК

Status: Accepted

## Context

ПЭК уже имеет типизированные тарифы, предварительное оформление,
аннулирование, tracking, ПВЗ и PDF-формы. Возврат принятого груза был
намеренно fail-closed, хотя официальный API предоставляет отдельный
`/cargos/cancelandreturncargo/` с bounded запросом одного кода груза.

## Decision

Допустить для ПЭК существующий provider-neutral `logistics.return.create`
через durable return-logistics worker. Адаптер принимает только
`mail_type=pek_cargo_return`, один числовой `original_remote_id` и вызывает
официальный `POST /api/v1/cargos/cancelandreturncargo/` с `{ "code": ... }`.
Только `success=true` нормализуется как `created` с тем же remote ID. Ответ
`success=false` является подтверждённым конфликтом; timeout или обрыв
соединения не трактуются как отказ и оставляют операцию в неизвестном
состоянии для сверки.

## Security and privacy impact

Basic credentials остаются callback-scoped в SecretProvider. В operation,
audit и Core не попадают provider description или сырой response. Один
внешний код груза и tenant-scoped operation receipt ограничивают область
воздействия; capability, account status и существующая approval-политика
проверяются до worker-вызова.

## Compatibility impact

Изменение аддитивное: добавляется capability в существующий ПЭК manifest и
runtime support. Публичный endpoint возврата уже существует и его OpenAPI
контракт не меняется. Миграции не требуются.

## Operational impact

Оператор должен иметь подтверждённый расширенный кабинет ПЭК и подключённую
услугу возврата. ПЭК может принять запрос в очередь; повторная сверка должна
использовать тот же provider cargo code. Отмена сформированного груза,
адресная доставка, пакетная печать и webhooks не включаются этой ADR.

## Alternatives considered

Сохранить возврат fail-closed отвергнуто: официальный payload и response
известны, а общий worker уже обеспечивает idempotency и unknown-outcome
границу. Переиспользовать `/order/cancellation/` отвергнуто: это другая
операция, применимая к предварительному оформлению. Принимать `success=false`
как успех отвергнуто: документация ПЭК явно различает успешный и неуспешный
возврат.

## Consequences

ПЭК получает bounded возврат уже принятого груза через единый API/UI-контур;
provider-specific схема остаётся внутри адаптера. Для боевого допуска нужен
отдельный тестовый груз и live qualification, поскольку возврат меняет
маршрут груза и может быть недоступен на отдельных стадиях перевозки.
