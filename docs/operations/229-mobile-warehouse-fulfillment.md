# Мобильная и складская работа: эксплуатация Task 229

## Что доступно

Раздел frontend «Мобильный склад» открывается по адресу
`/warehouse/mobile`. Он показывает планы FBS/FBO/hybrid/split, очереди pick,
упаковки, печати, устройства и offline receipts. На узком экране доступны
сканирование, создание pack session, закрытие упаковки и постановка печати.

FBS проходит локальную цепочку:

```text
order/allocation → WMS pick task → mobile scan → pack → print → handoff → tracking
```

FBO показывает только inbound/order visibility и remote observations. В нём
нет локальной кнопки pick, pack или print. Hybrid/split используют отдельные
seller-owned планы и не смешивают remote-owned stock с локальным заданием.

## API и SDK

Основные операции находятся под `/api/v1/warehouse-mobile`:

- `GET /summary`, `GET /plans`, `POST /plans`;
- `GET /plans/{plan_id}`, `POST /plans/{plan_id}/pick-batches` и `/advance`;
- `GET/POST /batches`, `POST /scans`;
- `GET/POST /packs`, `POST /packs/{pack_id}/close`;
- `GET/POST /print-jobs`, `POST /print-jobs/{print_job_id}/status`;
- `GET/POST /devices`, `POST /devices/{device_id}/revoke`;
- `GET/POST /offline-intents`, `POST /observations`.

Каждая запись требует `Idempotency-Key`, tenant/workspace из auth context,
`wms.write` и expected version там, где изменяется состояние. В Go, Python и
TypeScript SDK операции генерируются из OpenAPI. Raw code нужен только для
проверки команды и не возвращается: response содержит `code_digest`.

## Сканирование, упаковка и печать

Скан сначала проходит проверку типа кода, устройства, location, количества и
версии WMS task. Для product/serial canonical WMS scan выполняется до записи
mobile evidence. Повтор с тем же ключом безопасен; другой payload с тем же
ключом — conflict. Сверхпик, wrong SKU/location и revoked device не закрывают
задание автоматически.

Pack session хранит integer grams/mm и digest фактов. Pack по batch нельзя
открыть, пока входящие WMS tasks не завершены; изменение фактов создаёт новую
печатаемую intent, старая история не переписывается. Print queue различает
`queued`, `printed`, `failed`, `unknown`, `cancelled`; timeout принтера не
считается печатью.

## Offline и восстановление

Offline разрешён только для ограниченного preloaded scan context. На клиенте
держится короткоживущая bounded очередь без токена и лишних customer данных.
Reconnect передаёт sequence, version, idempotency key и digest. `pending`,
`rejected`, `unknown` и `needs_attention` отображаются явно. После revoke
устройства старые intents не применяются.

При crash или timeout:

1. не повторять неизвестный print/handoff вслепую;
2. проверить server receipt и remote status по correlation/idempotency key;
3. оставить evidence и создать manual-attention/reconciliation action;
4. продолжить только после подтверждения версии и владельца операции.

## Контроль и квалификация

Локальный gate:

```bash
make mobile-warehouse-qualification
```

Он проверяет migration `000056`, catalog hash, OpenAPI/SDK/API wiring, event
schemas, RLS/append-only boundary, FBO policy и frontend. Нагрузочный smoke
использует синтетические данные и не является live qualification.

Отдельно на целевой topology нужны redacted evidence для официального FBS и
FBO sandbox/live API, shipment/label/tracking, а также конкретных профилей
scanner/camera, scale и printer. До их появления в Integration Center нельзя
показывать `qualified` и нельзя выдавать remote FBO operation за локальную.

Общий release-runner gate для финансового и складского контура —
`make financial-warehouse-qualification`. Он выполняет локальные
`financial-completeness-qualification` и `mobile-warehouse-qualification`,
после чего fail-closed проверяет 12 внешних retained evidence artifacts.
Для `fbs` и `fbo` обязательны exact scopes, fulfillment/label/tracking/handoff
или inbound/acceptance/return checks, read-after-write и reconciliation. Для
`hardware` обязательны profile version, discovery/pairing/health,
timeout/retry, scan/camera/scale/print и safe fallback на совпадающей
`topology_ref`. Дополнительно обязательны partner UAT, rollback с restore и
replay-проверкой, SLO/DR rehearsal и production support/on-call/escalation
evidence. Без актуального credentialed sandbox evidence UI/Integration Center сохраняет
`read_only`/`qualification_required`.
