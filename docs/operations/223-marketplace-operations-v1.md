# Marketplace Operations v1 — эксплуатационный контур

## Назначение

Epic 176 (`tasks/issues/223-marketplace-operations-v1.md`) — это сквозной
release gate для marketplace, а не отдельный provider adapter. Он собирает
состояние кабинета, каталога, цен, остатков, заказов, WMS, отгрузок, возвратов,
маркировки, settlement и P&L.

## Статусы поддержки

- `read_only` — разрешены только подтверждённые чтения;
- `partially_supported` — часть операций допущена, остальные явно denied;
- `qualified` — полный заявленный сценарий прошёл synthetic/Docker и
  provider-specific live qualification.

Статус вычисляется по capability и evidence. Наличие manifest, SDK-типа,
credentials или health-check не делает кабинет `qualified`.

## Рабочий поток

```text
account → product → publication → price/stock → order → reserve
→ pick/pack → shipment → return → settlement → P&L
```

Каждый шаг должен иметь tenant scope, operation/idempotency reference,
normalized status, audit lineage и reconciliation path. Операция с timeout
после удалённого принятия получает `unknown`; оператор сначала запускает
reconciliation/status read, а не повторяет write вслепую.

## Перед включением capability

1. Проверить account state, актуальность credentials и required auth scopes.
2. Проверить connector capability и provider qualification evidence.
3. Проверить policy/approval, лимит массовой операции и idempotency key для
   write-sensitive action.
4. Проверить backup/migration gate дочернего runtime slice.
5. Выполнить dry-run или preflight и убедиться, что stale/missing mappings
   видны оператору.

## При сбое

- `timeout`/неизвестный ответ: остановить повторную запись, создать
  `unknown`/`manual_attention`, выполнить status read и reconciliation;
- `rate_limit`: сохранить bounded retry-after и не обходить лимит параллелизмом;
- `reauthorization_required`: отключить затронутые writes, не выводить токен в
  ошибку, обновить credential через SecretProvider;
- mismatch order/stock/price/settlement: не исправлять исторический факт,
  сформировать append-only finding;
- падение worker: восстановить lease безопасным replay; внешнюю операцию с
  неизвестным результатом не повторять без evidence.

## Критерий выпуска

Для каждого provider отдельно публикуются capability matrix, redacted
qualification evidence и дата актуальности. До прохождения полного сценария
интерфейс обязан показывать `read_only` или `partially_supported` и скрывать
неразрешённые mutation actions.
