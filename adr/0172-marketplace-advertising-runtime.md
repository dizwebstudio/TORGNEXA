# ADR-0172: Marketplace Advertising Runtime для WB и Ozon

Status: Accepted

## Context

Task 050 уже содержит provider-neutral advertising planning, а финансовый
контур Task 174 умеет учитывать advertising facts. При этом реальные расходы,
показы, клики, заказы и выручка WB/Ozon не загружались, поэтому P&L не мог
показать подтверждённую рекламу и её качество. Управляющие операции опасны до
стабилизации чтения и не должны появиться как кнопки без approval.

## Decision

Ввести provider-neutral advertising domain и tenant-scoped PostgreSQL
projection для Campaign, AdGroup, Ad, CampaignProduct, SpendFact,
PerformanceFact, Attribution, sync run и reconciliation finding. WB/Ozon
адаптеры реализуют только `ads.read`; официальные ответы преобразуются в
bounded UTC facts с ISO currency, source reference и quality.

Worker ежедневно загружает предыдущий полный UTC-день и сохраняет watermark;
повтор безопасен по account/period/mode и remote fact identity. Spend facts
передаются в существующий financial input. При наличии API fact старые
advertising action/settlement copies исключаются из текущего расчёта, но не
удаляются, чтобы provider-total/local-total reconciliation оставалась
воспроизводимой. Неразнесённые и задержанные факты публикуются как findings.

Read API и UI показывают campaign, spend, performance, metrics, quality,
reconciliation и sync runs. Все записи фактов append-only, tenant-scoped и
без raw payload или credentials. ClickHouse может быть только disposable
projection. Campaign writes, budget/bid/status/product mutations остаются
вторым этапом после capability audit, policy/approval, caps, idempotency,
read-after-write и unknown-result qualification.

## Consequences

P&L получает реальные рекламные расходы и видит ROAS/ROMI/ДРР без двойного
учёта. Неполная статистика и отсутствие SKU остаются видимыми. Цена — отдельная
reconciliation с settlement и live credential qualification; управление
кампаниями нельзя считать готовым по одному наличию SDK типов.

## Alternatives considered

Добавить provider branches в Core отклонено: провайдерские различия принадлежат
адаптерам. Читать каждый раз напрямую из API отклонено: это нестабильно,
неаудируемо и мешает P&L. Складывать API spend поверх settlement без выбора
источника отклонено из-за двойного расхода. Разрешать writes одновременно с
read MVP отклонено из-за approval и неизвестного результата.

## Compatibility impact

OpenAPI и Go/Python/TypeScript SDK расширяются аддитивно. Добавляется только
`ads.read` для WB/Ozon; существующие marketplace products/inventory paths не
меняют контракт. Финансовая модель получает новый normalized source и
сохраняет прежние payout/settlement semantics.

## Migration and data impact

Migration `000047_marketplace_advertising_runtime.sql` — expand-only,
high-risk, backup-gated. Она создаёт tenant-scoped campaign/fact/evidence/run
tables с forced RLS и append-only triggers; существующие ledgers не
переписываются. Down migration не применяется.

## Security and privacy impact

SecretProvider остаётся единственным источником токенов. Raw provider bodies,
Authorization headers, secrets и лишняя PII не входят в facts, events, logs,
P&L или API. Все чтения проходят authenticated tenant scope, capability
`ads.read` и bounded limits.

## Operational impact

Нужны метрики freshness/lag, partial runs, duplicate suppression и findings.
При сбое run становится `partial`, worker не блокирует транзакции. Откат —
отключение capability и drain worker; повторная загрузка создаёт только
идемпотентные факты или новую версию evidence, не изменяя старые строки.
