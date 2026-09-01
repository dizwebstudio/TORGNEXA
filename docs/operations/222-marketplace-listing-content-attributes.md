# Marketplace-карточки: контент и атрибуты

В репозитории карточка собирается из канонических Product/Offer/PIM-фактов и
не дублирует товарную истину в connector-коде.

## Рабочие места оператора

- `/catalog` — название, описание, SKU/Offer, цены, категории, изображения и
  их порядок. Загрузка изображения проходит через quarantine/release boundary;
  изменение использует версию записи.
- `/publication-quality` — readiness, blockers, warnings, compliance и
  capability/freshness evidence перед публикацией.
- `/marketplace-publications` — snapshot preflight, dry-run, approval reference,
  очередь публикации, remote status и reconciliation drift.
- `/marketplace-listings` — taxonomy канала, conditional attributes, локализованный
  content, media release, batch preview до 1 000 SKU, approval-gated apply и
  read-after-write reconciliation.

Публикация не считается успешной по одному HTTP-ответу: состояния `unknown`,
`needs_attention` и `rejected` остаются видимыми оператору.

## Что работает сейчас

В repository-контуре доступны:

- provider-neutral taxonomy с версиями, fingerprint и freshness;
- required/optional/conditional attributes, enum deprecation, типы, units,
  диапазоны и диагностические remediation;
- deterministic mapping с enum map, trim/case и exact taxonomy check;
- localized title/description/bullets, variants/SKU и только released/safe
  media references;
- bounded deterministic batch preview с before/after digest, сортировкой SKU,
  лимитом 1 000 и исключением blocked rows;
- approval-gated durable batch journal, OpenAPI, Go/Python/TypeScript SDK и
  MCP dry-run preview;
- approved remote plan до 1 000 строк, отдельные immutable publication
  snapshots/operations и `remote_operation_ids` с per-row idempotency;
- typed taxonomy-reader, listing-writer и status-reader ports с dry-run,
  idempotency и нормализованным unknown outcome;
- read-after-write reconciliation для mismatch, missing, status и unknown;
- tenant-scoped append-only persistence с RLS, idempotency и migrations 52/59;
  migration 59 добавляет безопасную асинхронную remote identity для operation
  ID в observations и drifts.

Demo taxonomy намеренно синтетическая. Для WB, Ozon и Yandex Market нельзя
подставлять её вместо официальной схемы канала.

## Release gate

`products.write` и batch API не являются доказательством live-поддержки. Для
каждого коннектора отдельно нужны официальные channel-specific схемы, scope и
rate-limit evidence, тестовый remote batch write и credentialed read-after-write.
Repository runtime закрывает очередь и автоматический status read-after-write;
до live evidence UI показывает qualification boundary, а не успешную
production-поддержку.

MCP/OpenClaw может только запросить preview. Apply, approval и remote write
остаются за authenticated HTTP/API boundary.

## Безопасность

Raw provider payloads, токены и приватные ключи не попадают в PIM, snapshots,
events, логи или UI. AI может подготовить draft, но не публикует карточку и не
обходит quality/compliance/approval.
