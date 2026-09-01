# Marketplace listing remote runtime

Task 232 закрывает repository-контур между массовой карточкой и remote
publication runtime.

## Что теперь работает

- taxonomy получает типизированный provider profile для WB, Ozon и Yandex
  Market: тип ID категории, ключ атрибута, режим batch apply и способ
  read-after-write;
- `POST /api/v1/marketplace-listings/batch/apply` принимает approved
  `publications` plan до 1 000 строк;
- каждая строка проверяет tenant/account/SKU/category, актуальный
  publication-quality receipt и typed operation;
- snapshot и operation сохраняются в существующий marketplace publication
  runtime, с отдельными idempotency keys и `remote_operation_ids` в batch
  evidence; `remote_id` и `remote_operation_id` сохраняются до claim worker,
  snapshots и operations создаются одной транзакционной batch-записью после
  фиксации batch-журнала;
- worker вызывает connector writer, затем status reader, поэтому `unknown`,
  `processing`, partial и rejection остаются видимыми состояниями; normalized
  observations и drifts сохраняются append-only;
- `/marketplace-listings` показывает форму remote plan и число операций,
  поставленных в durable queue.

## Как пользоваться

1. Получить taxonomy и собрать preview.
2. Получить approval на конкретный preview.
3. Для каждой eligible строки передать immutable publication snapshot и
   quality receipt в `publications`; для асинхронного status-read передать
   `remote_operation_id`, для update/lifecycle — существующий `remote_id`.
4. Передать тот же preview с `Idempotency-Key` и `Approval-Request-ID`.
5. Отслеживать `remote_operation_ids` через
   `/api/v1/marketplace-publications` и drifts операции.

Пустой `publications` оставляет совместимый evidence-only режим и не вызывает
remote write. MCP не получает права на apply.

Для live remote apply перед первым вызовом действует общий fail-closed
qualification gate: наличие manifest capability или health-check недостаточно.
Каналы со статусом `ready`, но без credentialed evidence, получают
`provider_qualification_required`; dry-run не выполняет сетевой side effect.
Create не принимает заранее заданный remote ID, а status-read обязан иметь
хотя бы один из `remote_id` или `remote_operation_id`.
Raw provider payloads, токены, Authorization-заголовки и произвольные URL не
сохраняются в snapshot, batch evidence, событиях, логах или API-ответах.

## Граница квалификации

Provider profile — это контракт адаптера, а не подмена официальной схемы.
WB/Ozon/Yandex Market остаются `qualification_required`, пока для конкретной
версии API не сохранены redacted credentialed evidence официальной taxonomy,
scopes, mapping, лимитов и read-after-write. Синтетическая conformance не
переводит канал в production-ready.

Для retained evidence есть отдельный структурный gate:

```bash
TORGNEXA_MARKETPLACE_EVIDENCE_FILE=/abs/path/marketplace-remote-qualification.json \
TORGNEXA_MARKETPLACE_EVIDENCE_SCOPE=listing \
make marketplace-remote-evidence
```

Он проверяет только redacted JSON и намеренно не выполняет удалённые вызовы.
Пример в `qualification/marketplace-remote-qualification.example.json` —
синтетический шаблон, а не подтверждение готовности WB/Ozon/Yandex Market.
Credentialed taxonomy, batch write и read-after-write по-прежнему являются
внешним release gate.

См. [Task 232](../../tasks/issues/232-marketplace-listing-remote-runtime.md) и
[ADR-0183](../../adr/0183-marketplace-listing-remote-runtime.md).
