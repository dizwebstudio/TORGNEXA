# Центр качества публикации товаров

Центр качества публикации — это target-specific preflight перед записью
товара в connector account. Он не заменяет Product/PIM, цены, остатки,
медиабезопасность или compliance: эти домены остаются источниками фактов, а
центр хранит только производную проверку и её lineage.

## Что проверяется

Проверка собирает bounded snapshot товара/offer и применяет версионированный
declarative profile канала:

- идентификаторы, SKU/GTIN, название и описание;
- категорию и обязательные атрибуты;
- released и safe media, формат, размер и количество;
- цену, валюту и доступный остаток;
- mapping и admitted/enabled capability аккаунта;
- локаль, юрисдикцию и channel contract;
- актуальное решение Task 082 compliance.

Один и тот же товар может быть `ready` в одном аккаунте и `blocked` в другом.
В профиле разрешены только типизированные правила (`required`, `enum`,
`max_length`, `min_value`, `max_value`); код, SQL, HTTP, regex-исполнение и
секреты профилю недоступны.

## Решения и score

`ready` и `ready_with_warnings` допускают публикацию. `blocked`,
`approval_required`, `stale`, `unsupported`, `not_configured` и `unknown`
запрещают автоматическую запись. Score и category scores — объяснимая
производная метрика в basis points; они не могут перевесить hard blocker.

Каждый успешный run выдаёт `PublicationGateReceipt`. Receipt связывает target,
account, версии Product/Offer/price/inventory/media/mapping/capability,
snapshot/profile digests и compliance fingerprint. `commerce-sync` проверяет
актуальный receipt непосредственно перед remote egress; изменение любого
входа или истечение TTL делает receipt непригодным.

## API для оператора

Все операции tenant-scoped и требуют permission `products.read`:

```text
GET /api/v1/publication-quality/runs?product_id=<id>&limit=50
GET /api/v1/publication-quality/runs/<run_id>
GET /api/v1/publication-quality/receipts/<receipt_id>
```

Ответ run содержит decision, score, category scores и bounded issues с
`field_path`, expected/observed и remediation hint. API не принимает tenant
идентификаторы из тела запроса и не является обходом policy/approval.

Контракт профиля закреплён в
`contracts/publication-quality/profile.schema.json`. Профиль может содержать
только типизированные проверки из allow-list; неизвестные поля,
дублирующиеся идентификаторы и отсутствующие границы decimal-правил
отклоняются до активации. Исторические profile/ruleset версии не изменяются.

Для исправления центр создаёт только `RemediationAction` с
`expected_snapshot_digest` и `proposed_diff_digest`. Применение выполняется
обычным Product/PIM/API-командованием с idempotency и optimistic version;
чувствительные изменения проходят Task-017 approval. В базе proposal
append-only, поэтому повторная отправка с другим diff возвращает conflict.

## Хранение и эксплуатация

Миграция `000033_product_publication_quality.sql` добавляет профили, runs,
issues, receipts и remediation proposals. Таблицы имеют organization/workspace
ключи, forced RLS, ограниченные индексы и append-only evidence для issues,
receipts и remediation. Не сохраняются provider payloads, бинарные media,
Authorization headers или credentials.

Перед применением миграции нужен обычный backup checkpoint. Для отката
отключается вызывающий gate; исходные catalog/PIM/inventory/compliance данные
не переписываются. Наблюдаемость должна показывать latency evaluation,
распределение решений, stale/unknown rate, категории issues и причины отказа
receipt.

Минимальные operational alerts: рост `quality_gate_denied` или `unknown`,
stale-rate выше 20% за 15 минут, отсутствие свежих runs для активного target,
рост конфликтов receipt и задержка outbox/commerce-sync. При всплеске ошибок
сначала отключается outbound policy конкретного workspace, затем проверяются
profile digest, compliance freshness и consumer lag; SQL-изменения в evidence
не используются. Восстановление — повторный bounded preflight после
устранения причины, а не переиспользование старого receipt.

На небольшом Compose/VPS запуск ограничен 100 runs на страницу, 512 rules на
profile, 64 media refs на snapshot и 1024 issues на run. Cache/Valkey не
участвует в принятии решения; при недоступной БД или неоднозначном receipt
публикация блокируется.

## Локальная проверка

```bash
docker compose -f deploy/compose/docker-compose.yml run --rm migrate
docker run --rm -v "$PWD":/app -w /app golang:1.26-bookworm \
  bash -lc 'export PATH=/usr/local/go/bin:$PATH; go test ./internal/platform/publicationquality ./internal/platform/postgres/publicationqualityrepo ./internal/app/api ./internal/app/worker'
```

Для e2e сначала создайте канонические Product/Offer, mapping, capability и
compliance evidence; без них корректный результат центра — `unknown` или
`blocked`, а не искусственный pass.
