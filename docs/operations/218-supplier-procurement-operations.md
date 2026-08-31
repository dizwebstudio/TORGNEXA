# Закупки: поставщики, прайс-листы и заказы

Epic 173 добавляет рабочий экран закупок поверх существующих контуров
LegalParty, replenishment, PurchaseOrder и WMS.

## Рабочий поток

1. В «Контрагентах» создаётся или находится каноническая LegalParty.
2. В «Закупках» заводится supplier profile с её `legal_party_id` и
   операционными условиями.
3. Прайс-лист загружается обычным upload pipeline. Пока файл не получил
   состояние `released`, procurement его не читает.
4. Preview показывает валидные, ошибочные и несопоставленные строки. Matching
   идёт в порядке GTIN, supplier SKU, затем explicit manual mapping.
5. Commit применяется только после проверки preview. История прежней цены
   остаётся append-only.
6. Черновик PO создаётся вручную или из рекомендации пополнения. Для
   рекомендации сохраняется digest snapshot; устаревший snapshot отклоняется.
7. Approve и send требуют согласование по тому же PO. Timeout не считается
   отправленным: PO получает `send_state=unknown` и попадает в reconciliation.
8. Приёмка записывает факт количества и передаёт его WMS. Остаток меняется
   только WMS ledger.

## API surface

- `GET/POST/PATCH /api/v1/procurement/suppliers`
- `GET/POST /api/v1/procurement/offers`
- `POST /api/v1/procurement/price-lists/preview`
- `POST /api/v1/procurement/price-lists/commit`
- `GET/POST /api/v1/procurement/purchase-orders`
- `POST /api/v1/procurement/purchase-orders/from-recommendations`
- PO actions `approve`, `send`, `retry`, `send-timeout`, `cancel`, `receive`
- `GET /api/v1/procurement/reconciliation`

Все mutation endpoints требуют `Idempotency-Key`; отправка и юридически
значимые операции требуют matching approval. API возвращает нормализованные
состояния и не отдаёт raw upload/provider payload.

## Runtime boundary

Реализованы provider-neutral supplier/offer/PO records, CSV/XLSX parser,
preview/commit, audit/outbox, receiving evidence и reconciliation для
синтетических данных. Реальные интеграции ГИС МТ, ЭДО, ККТ/ОФД и marketplace
заказов остаются `deferred` до отдельной conformance qualification.

Migration 45 expand-only, high risk, требует backup. Для отката используется
отключение capability и drain worker; destructive down migration не применяется.
