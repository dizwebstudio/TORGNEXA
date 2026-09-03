# Production qualification evidence

`make production-qualification` is the P2 deployment gate. It fails closed when Docker is unavailable and records evidence for the exact topology it exercised.

The gate performs:

1. the deterministic Task-066 SLO regression;
2. a fresh isolated Compose deployment;
3. Outbox -> Kafka -> Inbox end-to-end delivery using the application image;
4. duplicate immutable-event delivery followed by a marker event to prove Inbox idempotency and continued consumer progress;
5. a durable `LOST` warehouse incident with an explicit backup route, positive backup ATP, append-only routed evidence, and proof that source physical stock is unchanged;
6. black-box API load against the Task-066 API availability/p99/throughput limits;
7. graceful worker restart;
8. Kafka restart/recovery;
9. PostgreSQL restart/recovery;
10. repeat runtime probes, including warehouse incident automation, after every failure drill.

Generated evidence is written below `qualification/evidence/<UTC timestamp>/` and is intentionally not committed. Production release evidence must be retained by the release system together with image digests and infrastructure metadata.

## P4 go-live qualification

`make p4-qualification` is the final go-live evidence synthesizer. Unlike P2/P3 repository/topology gates, it intentionally requires external facts and therefore normally runs from a protected release/change runner, not a developer workstation.

Required inputs:

- exact clean Git tag `v$TORGNEXA_P4_VERSION` and Go 1.26.7;
- Docker Compose v2 for the full P3 topology/restart/restore/upgrade drills;
- `TORGNEXA_P4_REPOSITORY=OWNER/NAME` and `TORGNEXA_P4_PROTECTED_BRANCH`;
- optional `TORGNEXA_P4_GITHUB_TOKEN` for private GitHub repositories; it is never retained;
- HTTPS `TORGNEXA_P4_BASE_URL` plus environment-only `TORGNEXA_P4_BEARER_TOKEN`;
- absolute `TORGNEXA_P4_CONNECTOR_PLAN`, based on `qualification/live-connectors.example.json`;
- absolute `TORGNEXA_P4_POSTURE_FILE`, based on `qualification/production-posture.example.json`;
- absolute downloaded/unpacked `TORGNEXA_P4_RELEASE_EVIDENCE_DIR` from the exact protected release workflow;
- environment-only `TORGNEXA_P4_GITHUB_RELEASE_TOKEN` with Contents read access during qualification so P4 can bind local evidence to staged draft asset digests; promotion requires the same variable to hold a token permitted to update the draft release;
- absolute `TORGNEXA_P4_SECURITY_TOOLS_DIR` containing the checksum-verified `cosign` used for independent verification.

The GitHub branch-rules capture must prove an active ruleset workflow pinned by SHA to `.github/workflows/architecture-required.yml`, deletion/force-push protection, pull-request approvals and a required Team reviewer for architecture paths. It is not possible to replace those hosted facts with a local `PASS` flag.

Live connector plans contain account/connector IDs and the exact capability
names that the run is expected to exercise; they never contain credentials or
secret references. Every connector account that is `active` in the target
tenant must be listed; omission fails P4. Each listed account must have a
configured opaque SecretProvider reference, every `required_capabilities` entry
must be enabled on the account, and the runner performs two consecutive remote
health checks. This proves credentialed runtime reachability and account
configuration; it does not promote a business capability to `qualified` and
does not prove remote read-after-write. That requires the separate marketplace
evidence gate below. `run_sync` is off by default; when deliberately enabled,
the caller must additionally set
`TORGNEXA_P4_ALLOW_REMOTE_SYNC=I_UNDERSTAND_THIS_MAY_WRITE` because the account's
active sync policy may write provider state.

A successful run writes `p4-go-live.json` plus digested subordinate evidence below `qualification/evidence/p4-<UTC>/`. This directory is not source material and must remain ignored by Git and Docker build contexts.

After a retained P4 PASS, public publication is a separate explicit operation: `TORGNEXA_P4_GO_LIVE_EVIDENCE=/abs/path/p4-go-live.json TORGNEXA_P4_GITHUB_RELEASE_TOKEN=... make p4-publish`. The promoter re-verifies the P4 root and every subordinate hash, requires the exact clean release tag, proves that the draft still has exactly the verified asset set with unchanged digest/size, uploads `p4-go-live.json` as the final audit asset, and only then clears the draft flag.

## Marketplace remote qualification evidence

Task 223/232 используют отдельный fail-closed валидатор для сохранённого
redacted evidence. Он проверяет структуру, release SHA, taxonomy fingerprint,
capability statuses, обязательные сценарии, rollback и отсутствие полей,
похожих на секреты. Валидатор не обращается к WB/Ozon/Yandex Market и не
может заменить credentialed live run.

Для проверки listing-контура:

```bash
TORGNEXA_MARKETPLACE_EVIDENCE_FILE=/abs/path/marketplace-remote-qualification.json \
TORGNEXA_MARKETPLACE_EVIDENCE_SCOPE=listing \
make marketplace-remote-evidence
```

Для полного marketplace-цикла используется `SCOPE=full`; тогда evidence
обязан содержать order, reservation, pick/pack, shipment, returns/refund,
marking/EDO и P&L checks. В репозитории оставлен только синтетический пример
`marketplace-remote-qualification.example.json` для проверки формата. Его
нельзя выдавать за live qualification и нельзя помещать в него токены,
Authorization-заголовки, raw provider payloads или приватные URL.

## Что остаётся внешним release-gate

Репозиторные части Task 223/232, включая API, SDK, frontend, durable
publication operations, reconciliation projection и evidence validator,
закрыты. Открытыми считаются только проверки, для которых источник истины
находится вне Git:

- credentialed non-production taxonomy, batch remote apply,
  read-after-write и полный order → fulfillment → return → settlement/P&L
  сценарий для каждого заявленного marketplace;
- официальный EDO/маркировка, carrier и payment/fiscal smoke для полного
  сценария;
- Docker/PostgreSQL и точный Go toolchain runtime для deployment smoke;
- GitHub applied rules, Team reviewer, protected prerelease,
  OIDC/Sigstore/SLSA и release asset verification;
- production topology, backup/restore, on-call, rollback и device matrix.

Наличие manifest, SDK-типа, credentials или локального синтетического PASS не
закрывает ни один из этих пунктов. Такой результат появляется только после
сохранения redacted evidence из фактического окружения.

## Marketplace credentialed live smoke

Для минимального credentialed smoke WB/Ozon добавлен отдельный bounded runner:
`cmd/torgnexa-marketplace-live-smoke`. Он принимает provider credentials только
из environment release-runner, передаёт их адаптеру через `SecretAccessor`,
ограничивает HTTPS hosts и сохраняет только redacted evidence с правами `0600`.

Общие обязательные параметры:

```bash
export TORGNEXA_MARKETPLACE_SMOKE_CONNECTOR=wildberries # или ozon
export TORGNEXA_MARKETPLACE_SMOKE_ENVIRONMENT=non-production
export TORGNEXA_MARKETPLACE_SMOKE_TARGET=dedicated-non-production
export TORGNEXA_MARKETPLACE_SMOKE_ACCOUNT_REF=marketplace-sandbox-01
export TORGNEXA_MARKETPLACE_SMOKE_RELEASE_COMMIT=$(git rev-parse HEAD)
export TORGNEXA_MARKETPLACE_SMOKE_CATEGORY_CODE=123
export TORGNEXA_MARKETPLACE_SMOKE_OUTPUT=/absolute/path/marketplace-live-smoke.json
# The launcher maps the provider credential into this callback-scoped secret.
# For the two-line credential use: client-id<newline>api-key.
export TORGNEXA_MARKETPLACE_SMOKE_SECRET='credential-material'
export TORGNEXA_MARKETPLACE_SMOKE_WAREHOUSE_ID=dedicated-warehouse-id
export TORGNEXA_MARKETPLACE_SMOKE_VARIANT_ID=dedicated-variant-id
# To bind this smoke to the complete golden path, set all nine opaque refs:
export TORGNEXA_MARKETPLACE_SMOKE_FLOW_REF=golden-path/flow-01
export TORGNEXA_MARKETPLACE_SMOKE_ORDER_REF=order/flow-01
export TORGNEXA_MARKETPLACE_SMOKE_RESERVATION_REF=reservation/flow-01
export TORGNEXA_MARKETPLACE_SMOKE_SHIPMENT_REF=shipment/flow-01
export TORGNEXA_MARKETPLACE_SMOKE_RETURN_REF=return/flow-01
export TORGNEXA_MARKETPLACE_SMOKE_REFUND_REF=refund/flow-01
export TORGNEXA_MARKETPLACE_SMOKE_SETTLEMENT_REF=settlement/flow-01
export TORGNEXA_MARKETPLACE_SMOKE_MARKING_REF=marking/flow-01
export TORGNEXA_MARKETPLACE_SMOKE_EDO_REF=edo/flow-01
make marketplace-live-smoke
```

WB выполняет две health-проверки, bounded products/inventory/orders reads и
taxonomy read; его smoke намеренно read-only. Ozon выполняет тот же read-срез,
а в `qualification` scope дополнительно меняет остаток выбранного dedicated
non-production offer на `+1`, подтверждает read-after-write и восстанавливает
исходное значение. Для Ozon write scope требуется явное подтверждение:

```bash
export TORGNEXA_MARKETPLACE_SMOKE_ALLOW_WRITES=I_UNDERSTAND_THIS_IS_NON_PRODUCTION
```

Результат валидируется контрактом
`contracts/qualification/marketplace-live-smoke-v1.schema.json`. Команда не
создаёт live qualification без реальных credentials и подходящего
non-production account; при отсутствии внешнего доступа она завершается с
`FAIL`, сохраняя redacted failure evidence.

## Полный production golden path

Для release-runner добавлен объединяющий fail-closed gate:
`make production-golden-path`. Он сначала запускает
`make order-fulfillment-qualification`, а затем требует retained redacted
evidence, привязанный к текущему `HEAD` и одному общему `flow_ref`:

- полный marketplace evidence (`TORGNEXA_MARKETPLACE_EVIDENCE_FILE`, только
  `full` scope), credentialed marketplace live smoke и отдельный live
  return/refund/compensation artifact;
- отдельные credentialed non-production evidence для carrier, payment и
  fiscal, Chestny ZNAK и ЭДО (`TORGNEXA_CARRIER_GOLDEN_PATH_EVIDENCE_FILE`,
  `TORGNEXA_PAYMENT_GOLDEN_PATH_EVIDENCE_FILE`,
  `TORGNEXA_FISCAL_GOLDEN_PATH_EVIDENCE_FILE`,
  `TORGNEXA_MARKING_GOLDEN_PATH_EVIDENCE_FILE`,
  `TORGNEXA_EDO_GOLDEN_PATH_EVIDENCE_FILE`);
- aggregate manifest в
  `TORGNEXA_PRODUCTION_GOLDEN_PATH_EVIDENCE_FILE`.

Пример запуска на защищённом release-runner:

```bash
export TORGNEXA_PRODUCTION_GOLDEN_PATH_EVIDENCE_FILE=/evidence/production-golden-path.json
export TORGNEXA_MARKETPLACE_EVIDENCE_FILE=/evidence/marketplace-remote-full.json
export TORGNEXA_MARKETPLACE_LIVE_SMOKE_EVIDENCE_FILE=/evidence/marketplace-live-smoke.json
export TORGNEXA_MARKETPLACE_COMPENSATION_EVIDENCE_FILE=/evidence/marketplace-compensation.json
export TORGNEXA_CARRIER_GOLDEN_PATH_EVIDENCE_FILE=/evidence/carrier-golden-path.json
export TORGNEXA_PAYMENT_GOLDEN_PATH_EVIDENCE_FILE=/evidence/payment-golden-path.json
export TORGNEXA_FISCAL_GOLDEN_PATH_EVIDENCE_FILE=/evidence/fiscal-golden-path.json
export TORGNEXA_MARKING_GOLDEN_PATH_EVIDENCE_FILE=/evidence/chestny-znak-golden-path.json
export TORGNEXA_EDO_GOLDEN_PATH_EVIDENCE_FILE=/evidence/edo-golden-path.json
export TORGNEXA_P4_REPOSITORY=OWNER/NAME
make production-golden-path
```

Контракты aggregate и connector evidence находятся в
`contracts/qualification/production-golden-path-v2.schema.json`,
`contracts/qualification/connector-golden-path-evidence-v2.schema.json`,
`contracts/qualification/marketplace-remote-evidence-v2.schema.json`,
`contracts/qualification/marketplace-live-smoke-v2.schema.json` и
`contracts/qualification/marketplace-compensation-evidence-v2.schema.json`.
Gate проверяет SHA-256 всех восьми файлов, один release commit/repository,
совпадение connector/account refs и opaque order/reservation/shipment/return/
refund/settlement/marking/EDO refs во всех артефактах. Manifest дополнительно
требует полный путь order → reservation → pick/pack → label → shipment →
return → refund → settlement → P&L → reconciliation, отдельную
marketplace-компенсацию, live Chestny ZNAK, live ЭДО, фискализацию и
duplicate/out-of-order/timeout/approval/rate-limit/rollback checks. Он не
принимает synthetic fixture вместо внешнего evidence, не вызывает провайдеры
сам и не принимает credentials в аргументах или файлах.

`scripts/marketplace_compensation_evidence.py` — отдельная проверка redacted
артефакта, который должен быть создан фактическим non-production запуском
connector-а. В нём обязательны наблюдения `return=received`,
`refund=accepted`, `compensation=accepted`, `settlement=matched`,
read-after-write, idempotent replay и reconciliation. Артефакты Chestny ZNAK
и ЭДО проходят v2 connector validator с обязательными `status_read`/
`reconciliation` и `document_send`/`document_status`/`reconciliation`.
Поэтому отсутствие официальных credentials или провал любого внешнего вызова
оставляет release заблокированным.

## Внешняя квалификация финансового и складского контуров

Для Task 227/229 добавлен отдельный release-runner gate:
`make financial-warehouse-qualification`. Он запускает
`make financial-completeness-qualification` и
`make mobile-warehouse-qualification`, затем требует aggregate manifest и
восемь retained redacted evidence-файлов:

- `bank`, `acquirer`, `marketplace_payout`, `fx`, `advertising`;
- `fbs`, `fbo`, `hardware` — последний должен покрывать scanner, camera,
  scale и printer profile.

Минимальный запуск:

```bash
export TORGNEXA_FINANCIAL_WAREHOUSE_EVIDENCE_FILE=/evidence/financial-warehouse.json
export TORGNEXA_BANK_QUALIFICATION_EVIDENCE_FILE=/evidence/bank.json
export TORGNEXA_ACQUIRER_QUALIFICATION_EVIDENCE_FILE=/evidence/acquirer.json
export TORGNEXA_MARKETPLACE_PAYOUT_QUALIFICATION_EVIDENCE_FILE=/evidence/marketplace-payout.json
export TORGNEXA_FX_QUALIFICATION_EVIDENCE_FILE=/evidence/fx.json
export TORGNEXA_ADVERTISING_QUALIFICATION_EVIDENCE_FILE=/evidence/advertising.json
export TORGNEXA_FBS_QUALIFICATION_EVIDENCE_FILE=/evidence/fbs.json
export TORGNEXA_FBO_QUALIFICATION_EVIDENCE_FILE=/evidence/fbo.json
export TORGNEXA_HARDWARE_QUALIFICATION_EVIDENCE_FILE=/evidence/hardware.json
export TORGNEXA_P4_REPOSITORY=OWNER/NAME
make financial-warehouse-qualification
```

Контракты: `contracts/qualification/external-qualification-evidence-v1.schema.json`
и `contracts/qualification/financial-warehouse-qualification-v1.schema.json`.
Gate проверяет scopes, connector/profile version, release commit/repository,
account/profile refs, SHA-256, rollback, source dedup/unknown/reconciliation,
FBS/FBO read-after-write и hardware safe fallback. Он не вызывает внешние API и
не принимает credentials; без фактического non-production evidence production
claim остаётся заблокированным.
