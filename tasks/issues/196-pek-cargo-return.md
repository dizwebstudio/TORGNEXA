# Task 196: ПЭК — возврат принятого груза отправителю

## Status

`repository-complete` — 2026-08-31.

## Objective

Подключить одну документированную операцию ПЭК для уже принятого груза к
существующему provider-neutral `logistics.return.create` worker-контру без
расширения до отмены сформированного груза, адресного возврата или пакетных
операций.

## Deliverables

- `logistics.return.create` добавлен в manifest и runtime support только для
  ПЭК;
- SDK-адаптер принимает только `mail_type=pek_cargo_return`, один числовой
  `original_remote_id` и нулевой `tariff_code`;
- host transport вызывает официальный
  `POST /api/v1/cargos/cancelandreturncargo/` с одним полем `code`;
- принимается только `success=true`, а `success=false` становится
  подтверждённым provider conflict без сохранения текста ответа;
- сетевые ошибки остаются неоднозначными для durable worker, без повторного
  внешнего side effect в рамках одного operation receipt;
- добавлены детерминированные adapter/transport/runtime tests, UI-подсказка,
  матрица, спецификация, conformance/reconciliation документы, ADR и review.

## Scope limits

ПЭК выполняет возврат по одному коду груза только для подтверждённого,
расширенного кабинета с подключённой услугой «Возврат груза отправителю».
Провайдер может вернуть queued-результат; live qualification обязана
проверить повторную сверку. Отмена уже сформированного груза, адресная
доставка, пакетная печать и вебхуки остаются fail-closed.

## Sources

- [официальная документация ПЭК: операции с грузами](https://test-kabinet.pecom.ru/preweb/api/v1/help/cargos), раздел `/cargos/cancelandreturncargo/`;
- [официальный API ПЭК](https://test-kabinet.pecom.ru/preweb/api/v1).

## Verification

Run `gofmt`, targeted and full `go test`, `go vet`, contract and migration
checks, generated SDK/catalog checks, frontend typecheck/build and
`git diff --check`. Live credentialed qualification remains a separate gate.
