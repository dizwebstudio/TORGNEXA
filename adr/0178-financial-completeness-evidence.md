# ADR-0178: Evidence-контур финансовой полноты

Status: Accepted

## Context

P&L, cash-flow, settlement, FIFO, FX и advertising foundations уже есть, но
до Task 227 у отчёта не было единой проверки source coverage. Из-за этого
отсутствующий bank, COGS, FX, advertising или promotion fact мог быть принят
за обычный ноль.

## Decision

Финансовая полнота — это проверяемое наличие источников, дат, валюты и правил
распределения для выбранного basis (`order_accrual`, `settlement` или `cash`).
Отсутствующий, несопоставленный, устаревший или спорный факт остаётся таким
статусом и не подменяется нулём.

Existing Order, Payment, Refund, SettlementEntry, inventory/WMS, FX и
advertising facts остаются authoritative. Новый слой `financial_source_records`
хранит только redacted append-only evidence и source digest; второй денежный
ledger не создаётся. Bank account/statement tables содержат masked references
и указатель на SecretProvider, а не реквизиты или credentials.

Каноническая матрица опубликована в
`contracts/finance/financial-completeness-matrix-v1.json` и дублируется typed
функцией core для одинакового поведения API, worker и тестов. Источник
дедуплицируется по `(kind, source_system, account_ref, source_ref)`.
Совпадающий повтор безопасен, а изменение суммы, валюты или digest даёт
конфликт.

## Consequences

- Reports могут быть опубликованы в partial/stale/unmatched состоянии и
  показывают coverage и missing codes.
- Payout не считается revenue, а bank receipt не заменяет payout или payment.
- FX обязателен только для cross-currency evidence и ссылается на уже
  существующий immutable conversion snapshot (Tasks 089/131).
- COGS backfill создаёт новую версию расчёта; старый snapshot не переписывается.
- Реклама и промо должны либо иметь подтверждённую attribution policy, либо
  оставаться видимыми как unattributed.
- Live bank/marketplace qualification не может быть доказана локальным health
  check; она хранится как внешний redacted release evidence.

## Compatibility impact

Изменение аддитивное: новые API/SDK/MCP read surfaces, masked source import и
новая migration не меняют существующие Order, Payment, Settlement, FX и report
contracts. Технические status codes остаются стабильными.

## Migration and data impact

Migration 000054 — expand-only и backup-gated. Она добавляет tenant-scoped
bank account/statement metadata, source evidence, findings и bounded COGS
backfill jobs. Existing ledgers и snapshots не переписываются; source facts и
statements immutable, correction делается новой evidence/adjustment.

## Security and privacy impact

Все новые таблицы используют `FORCE ROW LEVEL SECURITY`. API принимает только
masked account reference и opaque SecretProvider locator, а секретные значения,
полные банковские реквизиты, raw provider payloads и PII отклоняются или не
возвращаются. Import/statement/backfill commit требуют permission,
Idempotency-Key и sensitive audit. MCP — read-only.

## Operational impact

Операторская вкладка показывает basis, coverage, freshness, quality, source
references и очередь findings. Preview → commit защищает statement и COGS
backfill от невалидного импорта. При недоступности источника предыдущий
подтверждённый snapshot не стирается; incident response использует disable/
kill-switch и reconciliation.

Локальный gate: `make financial-completeness-qualification`. Credentialed
sandbox/live bank, marketplace payout, FX и advertising checks проходят
отдельно на release topology.

## Alternatives considered

Определять полноту по наличию числа отклонено: это маскирует missing facts.
Создать второй financial ledger отклонено: canonical Order, Payment,
Settlement и inventory ledgers уже определяют истину. Использовать health-check
как доказательство bank/payout qualification отклонено: ping не подтверждает
загрузку выписки, matching или read-after-write.
