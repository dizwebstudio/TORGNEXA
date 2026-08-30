# Возвраты, отмены и refunds (Task 164)

## Границы и источник истины

Отмена заказа, физический возврат товара и возврат денег — разные операции.
Снимок `Order`/`OrderItem` не переписывается, а `payments.Refund` остаётся
единственным владельцем платёжного состояния. Связи и evidence хранятся в
PostgreSQL в tenant-scoped таблицах миграции `000029` с forced RLS.

`commerce_returns` хранит причину, источник, валюту и запрос доставки/налога.
`return_items` хранит exact decimal requested/received/accepted quantity и
disposition: `restock`, `quarantine`, `scrap`, `replace`. Одна строка может
возвращаться частями. `refund_allocations` связывает существующий refund с
return/order item и не позволяет сумме резервируемых allocation превысить
захваченную оплату.

## Канонические состояния

- cancellation: `requested -> approved -> executing -> cancelled|rejected|failed|unknown`;
  `unknown` означает неоднозначный результат удалённой операции и требует
  reconciliation/manual attention;
- return: `requested -> approved -> authorized -> in_transit -> received ->
  inspecting -> accepted|partially_accepted|rejected -> closed`, с
  `cancelled`/`expired` до необратимой стадии;
- refund: существующие `pending -> accepted -> succeeded|failed` плюс
  `unknown`/`manual_attention` для timeout или неразличимого ответа.

Переходы проверяются с optimistic `version`; повтор команды требует тот же
`Idempotency-Key`. Запрещены backward transitions, отрицательные amounts,
cross-currency и over-allocation.

## API

Новые tenant-scoped endpoints под `/api/v1`:

- `POST /order-cancellations`, `GET/PATCH /order-cancellations/{id}[/status]`;
- `GET/POST /returns`, `GET/PATCH /returns/{id}[/status]`,
  `POST /returns/{id}/inspection`;
- `POST /refund-allocations`.

Все mutating calls требуют `Idempotency-Key`, а ответы и ошибки не содержат
токены, raw provider payloads или платёжные реквизиты. Поля контракта описаны
в `contracts/openapi/torgnexa-v1.yaml`; события —
`commerce.orders.cancellation_requested.v1`,
`commerce.orders.cancellation_state_changed.v1`,
`commerce.returns.requested.v1` и `commerce.returns.state_changed.v1`.

## Approval и runtime qualification

Sensitive/legal действия проходят Task-017 policy/approval и повторную
проверку scope, capability, account, amount и version. После approval worker
может вызвать typed connector ports для carrier/payment и записать bounded
`commerce_operation_evidence`. Receipt/inspection должны предшествовать
складскому disposition; refund не является доказательством приёма товара.

Репозиторий и API готовы к детерминированной проверке, но production-статус
конкретного провайдера нельзя объявлять без live connector conformance,
retries/idempotency/rate-limit проверки и подтверждения WMS/fiscal/settlement
маршрутов. Неоднозначные remote outcomes переводятся в `unknown`, а не
повторяются вслепую.
