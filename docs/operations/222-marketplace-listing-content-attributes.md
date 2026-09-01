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

Публикация не считается успешной по одному HTTP-ответу: состояния `unknown`,
`needs_attention` и `rejected` остаются видимыми оператору.

## Граница текущей версии

В текущем repository slice нет притворной поддержки provider-specific taxonomy,
условных атрибутов, массового batch apply и live read-after-write для WB/Ozon/
Yandex Market. Эти возможности включаются только после официального connector
контракта, approval, idempotency, качества данных и квалификационного evidence.
До этого оператор работает с канонической карточкой, preflight и dry-run.

## Безопасность

Raw provider payloads, токены и приватные ключи не попадают в PIM, snapshots,
events, логи или UI. AI может подготовить draft, но не публикует карточку и не
обходит quality/compliance/approval.
