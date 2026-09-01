# Task 232 — Provider-specific taxonomy и remote listing runtime

## Статус

`repository-complete` — follow-up к Task 222/230 для закрытия разрыва между
provider-neutral карточкой и реальным remote runtime. PIM остаётся единственным
источником товарной правды; provider-specific данные остаются на connector
границе. Сначала закрывается repository-контур, затем отдельно проходит
credentialed qualification каждого канала.

```yaml
repository_status: complete
qualification_evidence_contract: validated
external_release_status: pending_external_evidence
```

## Repository completion evidence — 2026-09-01

- provider profiles для WB, Ozon и Yandex Market теперь типизированы и
  прикрепляются к taxonomy без provider payloads; content fingerprint сохраняет
  совместимость со старым evidence;
- approved `batch/apply` принимает bounded `publications` plan и проверяет
  tenant, account, SKU/category, operation и актуальный publication-quality
  receipt для каждой строки; create/update/status identity проверяется по типу
  операции;
- каждая строка плана сохраняется в существующие immutable snapshot и durable
  publication operation repositories с отдельными idempotency key и operation
  ID; `remote_id` и `remote_operation_id` сохраняются до claim worker, а
  batch-журнал фиксируется до одной транзакционной записи snapshots и
  operations; новый PIM или ledger не создаётся;
- существующий worker выполняет remote write и automatic status
  read-after-write через `ProductPublicationWriter`/`StatusReader`, записывает
  normalized observations/drifts и поэтому accepted/processing/unknown не
  маскируются под published;
- batch evidence возвращает `remote_operation_ids`, а `/marketplace-listings`
  показывает remote plan, число поставленных в очередь строк и связь с
  approval/quality evidence;
- OpenAPI описывает provider profile, remote publication plan и operation IDs;
  Python/TypeScript SDK уже передают эти additive поля через существующий
  `Any`/JSON body contract.

## Closure matrix

| Subtask | Result |
| --- | --- |
| 232.1–232.2 | Closed in repository: typed profile metadata, version/fingerprint/freshness and immutable taxonomy evidence. |
| 232.3 | Closed at the adapter-contract boundary: WB/Ozon/Yandex profiles and existing typed publication adapters are wired; provider-specific live schema evidence remains qualification-gated. |
| 232.4–232.5 | Closed: bounded approved plan, per-row stable idempotency, immutable snapshots, atomic durable operations, remote/async identity persistence and returned operation IDs. |
| 232.6–232.7 | Closed by the existing publication worker/status reader and normalized operation reconciliation; unresolved outcomes remain unknown/attention. |
| 232.8 | Closed: approval, quality receipt, tenant boundary and existing capability/secret gates are preserved before worker remote calls. |
| 232.9–232.10 | Closed: OpenAPI additive contract and `/marketplace-listings` remote-plan/progress UX. |
| 232.11 | Closed for repository contract/synthetic coverage; live provider evidence is separate. |
| 232.12 | Repository evidence gate closed: `make marketplace-remote-evidence` validates retained redacted listing evidence; credentialed official taxonomy, remote write and live WB/Ozon/Yandex read-after-write qualification remain external. |

The repository task is therefore closed without making an unsupported
production claim. `qualification_required` is intentionally visible in the
provider profile until redacted credentialed evidence is attached.

## Проблема

Task 222 уже содержит типизированную taxonomy, preview массовых изменений,
approval-журнал и normalized read-after-write контракт. Но одного сохранённого
демо-документа и ручной передачи `RemoteObservation` недостаточно: нужен
реальный adapter path от connector account до официальной taxonomy, массового
remote apply и подтверждения результата через status/read-after-write.

## Порядок работ

1. provider-specific taxonomy adapters и versioned evidence;
2. capability-aware runtime admission и snapshot conversion;
3. durable bulk remote apply с per-row receipts;
4. status polling, read-after-write и reconciliation;
5. API/OpenAPI/SDK/MCP и frontend progress/error UX;
6. conformance, demo/E2E и внешняя credentialed qualification.

## Подзадачи

### 232.1 — Provider taxonomy adapter contract

Добавить typed SDK-порты для чтения официальной категории, attribute schema,
enum/units, media slots и conditional rules. Adapter не отдаёт raw JSON,
credentials или provider URL; неизвестная схема завершается `unsupported`.

### 232.2 — Versioned taxonomy evidence

Хранить tenant-scoped taxonomy version, fingerprint, locale, jurisdiction,
source reference, observed/fresh-until и redacted evidence. Устаревшая или
изменившаяся схема блокирует mapping и remote apply до нового preview.

### 232.3 — WB/Ozon/Yandex provider mappings

Реализовать provider-specific преобразование normalized draft в typed connector
request для первой волны WB, Ozon и Yandex Market: category IDs, required
attributes, variants, barcode, dimensions, price и media capability. Неполный
mapping должен показываться как blocker, а не отправляться частично.

### 232.4 — Bulk remote apply plan

Преобразовать approved preview до 1 000 SKU в bounded remote plan с stable
per-row idempotency keys, concurrency/rate-limit policy, dry-run и отсутствием
повторной отправки неизменённых строк. План должен ссылаться на immutable PIM
snapshot, mapping и taxonomy fingerprint.

### 232.5 — Durable per-row receipts

Сохранять accepted/processing/published/rejected/unknown результат каждой
строки, remote ID/operation ID, attempt, error code и observed-at. Partial
результат не маскировать под полный успех; retry разрешать только для
retry-safe или разрешённого manual recovery сценария.

### 232.6 — Read-after-write resolver

После create/update автоматически вызывать typed status reader по remote ID или
operation ID. Для асинхронного провайдера polling имеет bounded timeout,
backoff и terminal states; timeout после принятия остаётся `unknown`.

### 232.7 — Reconciliation and drift queue

Сверять snapshot digest, category, attributes, media, remote ID и publication
state. Добавить missing, mismatch, stale taxonomy, rejected и unknown outcome
drifts с безопасным manual retry/rebuild preview.

### 232.8 — Approval, policy, quota and tenant boundary

Проверить capability, account health, SecretProvider, risk/approval,
idempotency, quota, kill switch и organization/workspace scope перед первым
remote call. MCP не получает apply-права и не может утвердить собственную
операцию.

### 232.9 — API/OpenAPI/SDK/MCP

Добавить typed endpoints для taxonomy refresh, bulk plan/apply, per-row
receipts, status polling и drifts. Сгенерировать Go/Python/TypeScript SDK;
MCP оставить read-only/dry-run.

### 232.10 — Frontend listing operations center

В `/marketplace-listings` показать источник и возраст taxonomy, provider
mapping blockers, preview digest, progress по строкам, remote IDs, unknown,
rejected, retry и reconciliation. Неподдерживаемые capability не получают
кнопку remote apply.

### 232.11 — Conformance and synthetic E2E

Добавить deterministic fake provider для taxonomy, 1 000 SKU, partial
response, duplicate idempotency, accepted-then-timeout, polling success,
remote rejection и read-after-write drift. Проверить RLS, redaction, audit и
replay.

### 232.12 — Credentialed release qualification

Для каждого канала отдельно сохранить redacted evidence официальной taxonomy,
scopes, mapping, batch write, rate limits, read-after-write, reconciliation и
rollback. Без этого статус остаётся `qualification_required`.

Репозиторная часть этого gate закрыта валидатором
`scripts/marketplace_remote_evidence.py` и командой
`make marketplace-remote-evidence`. Он проверяет только структуру retained
evidence, release SHA, capability statuses, обязательные checks, rollback и
redaction. Он не выполняет сетевые вызовы; example JSON синтетический и не
переводит connector в `qualified`. Внешняя часть закрывается только после
credentialed non-production run для конкретной версии API и сохранения
официального redacted evidence.

## Definition of Done

- 232.1–232.11 закрыты в репозитории с тестами, контрактами, документацией и
  frontend/API/SDK wiring;
- ни один remote write не выполняется без approved immutable preview и
  capability/SecretProvider/policy gates;
- каждый результат имеет per-row receipt и read-after-write decision;
- `unknown`, partial, stale и rejected не объявляются `published`;
- 232.12 закрывается только credentialed evidence для конкретного provider и
  версии API; синтетические данные не заменяют live qualification.

## Не входит

- добавление provider-specific полей в Product/PIM;
- сохранение raw provider payloads, токенов или произвольных URL;
- объявление WB/Ozon/Yandex production-ready без официального credentialed
  read-after-write evidence.

## Зависимости

222, 217, 226, 230, 010, 025, 029, 064.
