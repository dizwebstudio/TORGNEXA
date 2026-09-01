# ADR-0175: Marketplace listing workspace

Status: Accepted

## Context

Одна каноническая карточка Product/PIM должна иметь несколько projections для
разных marketplace. У каналов различаются taxonomy, обязательные и условные
атрибуты, enum, единицы измерения, media slots и асинхронные результаты записи.
Поля провайдера нельзя добавлять в Product, а batch-изменение на 1 000 SKU
нельзя выполнять без preview, approval и доказуемого read-after-write.

## Decision

Введён provider-neutral `marketplacelisting` workspace. Он содержит:

- versioned taxonomy с fingerprint, freshness, locale и jurisdiction;
- typed category/attribute/media-slot rules и conditional requirements;
- deterministic mapping с enum/unit transforms и защитой от stale taxonomy;
- listing draft с localized content, variants, released media references и
  AI provenance;
- bounded batch preview до 1 000 SKU с before/after digest и построчными
  blockers;
- append-only taxonomy/batch evidence в PostgreSQL с forced RLS;
- нормализованный connector port для taxonomy, listing write и status read;
- read-after-write reconciliation с `unknown` и drift-состояниями.

Массовое применение доступно только через HTTP approval boundary. MCP имеет
только dry-run preview: модель не может применить изменения или одобрить свой
запрос. Core не знает имена marketplace и не принимает raw provider JSON,
URL, токены или unreleased assets.

## API and UI

Добавлены OpenAPI/SDK endpoints:

- `GET /marketplace-listings/taxonomy`;
- `POST /marketplace-listings/batch/preview`;
- `POST /marketplace-listings/batch/apply`;
- `GET /marketplace-listings/batches/{batch_id}`;
- `POST /marketplace-listings/read-after-write`.

Операторский workspace доступен в `/marketplace-listings`: taxonomy,
conditional-attribute diagnostics, batch preview/apply status и
reconciliation отображаются в frontend.

## Compatibility and migration

Изменение additive. Migration `000052_marketplace_listing_workspace.sql`
добавляет две tenant-scoped immutable таблицы, unique idempotency, forced RLS,
append-only triggers и bounded JSON documents. Старые Catalog/PIM/publication
контракты не меняются.

## Compatibility impact

Существующие Product, Offer, PIM и publication API остаются совместимыми:
listing workspace хранит отдельную projection и не меняет канонические записи.
Новые OpenAPI routes и SDK методы добавляются аддитивно; неподдержанные
операции не включаются автоматически для уже подключённых кабинетов.

## Migration and data impact

Migration 52 является expand-only и требует проверенного backup перед
применением. Она добавляет tenant-scoped taxonomy и batch evidence, ограничивает
размер JSON-документов и не содержит destructive down migration. Публикуемые
снимки и результаты preview сохраняются как append-only доказательства.

## Security and privacy impact

В workspace попадают только bounded drafts, digests, ссылки на released media и
нормализованные diagnostics. Токены, raw provider payloads, произвольные URL и
непроверенные uploads отвергаются; HTTP apply требует approval, idempotency и
authenticated organization/workspace scope.

## Operational impact

Оператор видит свежесть taxonomy, построчные blockers, состояние batch и
reconciliation decision. Неизвестный remote outcome не считается публикацией;
его можно безопасно повторно сверить после проверки кабинета. Kill switch и
отключение capability остаются способом остановить новые remote writes.

## Alternatives considered

Встраивать channel attributes в Product отвергнуто: это смешало бы каноническую
товарную правду с разными схемами marketplace. Передавать batch напрямую из UI
тоже отвергнуто: без preview, approval и immutable evidence нельзя доказать
границы массового изменения и корректно обработать timeout.

## Consequences

Оператор получает единый безопасный workspace для подготовки карточек, а
marketplace-коннекторы получают общий typed boundary. За это live-публикация
остаётся отдельным qualification gate: demo taxonomy и SDK наличие не дают
права заявлять production-поддержку конкретного канала.

## Release boundary

Repository synthetic qualification закрывает доменную модель, API, UI, SDK,
MCP preview и batch limit. Production claim возможен только после отдельного
credentialed evidence для официальной taxonomy, channel-specific mapping,
remote batch write и read-after-write каждого marketplace. До этого capability
остаётся `qualification_required`, а не объявляется поддержанной записью.
