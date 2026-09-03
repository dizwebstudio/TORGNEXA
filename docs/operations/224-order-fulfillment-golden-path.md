# Task 224 — Golden path заказа и возврата

Task 224 закрывает repository-контур заказа: заказ → резерв → pick/pack →
этикетка → shipment → возврат → refund → settlement/reconciliation. Он
использует канонические Order, Inventory/WMS, Logistics, Return, Payment и
Settlement агрегаты и хранит только tenant-scoped orchestration references.

## Что доступно оператору

- В `Marketplace → Операции marketplace` видно состояние кабинета и его
  реальные capabilities.
- Workflow можно создать от начала либо с `start_stage=order`, если заказ уже
  материализован canonical importer-ом.
- Детальная страница workflow показывает текущую стадию, canonical lineage и
  redacted append-only timeline. Секреты, токены, raw carrier/payment payloads
  и штрихкоды в timeline не попадают.
- `unknown` и `needs_attention` не превращаются в повторный remote write:
  оператор сначала использует reconciliation/finding action.

## Контракт

Последовательность Task 224:

```text
order → reservation → pick_pack → label → shipment → return
→ refund → fiscalization → settlement → profitability → reconciliation → complete
```

Старт с заказа реализован через `marketplaceoperations.NewAtStage`. Команды
проходят существующий idempotent command journal; повтор с тем же ключом не
удваивает резерв, shipment, возврат или refund. Для внешнего timeout состояние
остаётся `unknown` до status read/reconciliation.

## API

- `POST /api/v1/marketplace-operations/flows` — создать flow. Для golden path
  можно передать `start_stage: order` и безопасную ссылку `{kind: "order", id}`.
- `GET /api/v1/marketplace-operations/flows` — bounded список процессов.
- `GET /api/v1/marketplace-operations/flows/{flow_id}` — flow и timeline.
- `POST /api/v1/marketplace-operations/flows/{flow_id}/commands` — команда
  текущей стадии с `Idempotency-Key`.
- `GET/POST .../findings` — очередь сверки и append-only действия оператора.

Публичные Go/TypeScript/Python SDK регенерируются из OpenAPI. Внешний side
effect выполняется только владельцем домена через typed connector capability,
policy/approval и durable worker.

## Qualification

```bash
make order-fulfillment-qualification
```

Gate покрывает synthetic полный путь, старт после materialization, требования
canonical references, duplicate/idempotency и unknown outcome. Sandbox/live
проверка официальных marketplace, carrier, payment/fiscal, Chestny ZNAK и ЭДО
connectors не подменяется тестовыми fixture: для неё нужны целевые
non-production accounts, scopes, topology и redacted retained evidence. До
этого соответствующие capabilities остаются `read_only`, `partially_supported`
или `qualification_required`.

## Production release gate

Полный release-runner gate выполняется командой:

```bash
make production-golden-path
```

Команда сначала выполняет provider-neutral synthetic qualification, затем
fail-closed проверяет aggregate evidence из
`TORGNEXA_PRODUCTION_GOLDEN_PATH_EVIDENCE_FILE` и восемь сохранённых redacted
артефактов: full marketplace qualification, credentialed marketplace smoke,
marketplace return/refund/compensation, carrier, payment, fiscal, Chestny ZNAK
и ЭДО. Каждый артефакт должен быть regular non-symlink JSON, иметь совпадающие
repository/release SHA/account refs и один и тот же redacted flow linkage;
SHA-256 должен быть записан в aggregate manifest. Manifest обязан содержать
PASS для всех стадий golden path, фискализации, live Chestny ZNAK, live ЭДО,
marketplace compensation, failure matrix, rollback и отдельный owner каждой
проверки.

Схемы evidence: `contracts/qualification/production-golden-path-v2.schema.json`,
`contracts/qualification/connector-golden-path-evidence-v2.schema.json`,
`contracts/qualification/marketplace-compensation-evidence-v2.schema.json` и
связанные v2 marketplace evidence schemas.
Внешний runner сохраняет только redacted evidence; токены, raw payloads,
remote IDs, штрихкоды и private signing material в него не попадают. Без
credentialed non-production evidence gate завершается ошибкой и release
остаётся заблокированным.
