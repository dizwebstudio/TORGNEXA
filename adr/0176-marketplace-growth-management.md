# ADR-0176: Управление акциями и рекламой через guarded growth runtime

Status: Accepted

## Context

TORGNEXA уже хранит read-only рекламные факты и умеет считать pricing guards,
но до этого не имел общего безопасного контура для акций, ставок, бюджетов и
жизненного цикла кампании. Эти операции финансово чувствительны: устаревшая
цена, неизвестная комиссия или timeout после удалённого принятия не должны
привести к слепому повтору.

## Decision

Ввести provider-neutral `marketplacegrowth` runtime. Он выполняет расчёт в
целых денежных minor units и basis points, формирует immutable preview до 1 000
SKU, проверяет floor price, minimum margin, freshness, stock risk, bid/budget
caps и eligibility, а затем создаёт только approval-bound idempotent intent.

PostgreSQL хранит правила, preview, операции, reconciliation drift и tenant
kill switch с forced RLS. Операция без credentialed connector qualification
получает состояние `qualification_required`; она не считается remote success.
Typed Connector SDK получает единый порт для promotion/advertising operation,
а concrete connector сам отвечает за mapping, timeout, retry-safe error,
read-after-write и unknown outcome.

API и frontend `/advertising` показывают кампании, акции, ставки/бюджеты,
массовый preview, расходы, reconciliation и kill switch. MCP разрешает только
read/preview и не может approve/apply.

## Consequences

Управление акциями и рекламой имеет единую проверяемую цепочку
`preview → approval → durable intent → remote qualification → read-after-write`.
Отсутствующие данные не превращаются в ноль, финансовый ledger не переписывается,
а частичный и неизвестный результат остаётся видимым оператору. До получения
актуальных credentials и официального evidence WB/Ozon остаются
`read_only`/`qualification_required`.

## Alternatives considered

Добавить операции непосредственно в существующий read-only advertising repo
отклонено: это смешало бы facts и commands. Универсальный `Invoke(map[string]any)`
отклонён: он ломает typed capability boundary. Разрешить remote writes из UI
до approval и read-after-write отклонено из-за риска двойного списания и
неизвестного состояния.

## Compatibility impact

Изменение аддитивное: добавлены OpenAPI operations, generated Go/Python/
TypeScript SDK methods, MCP preview descriptor и permissions `promotions.read`,
`promotions.manage`. Существующие advertising facts, settlement и P&L
контракты не меняются.

## Migration and data impact

Migration `000053_marketplace_growth_management.sql` — expand-only,
backup-gated, high-risk. Она создаёт versioned rule/preview, operation,
reconciliation и kill-switch tables с tenant RLS; previews/rules/drifts
append-only, operations обновляются только worker state transition.

## Security and privacy impact

В runtime не принимаются credentials, Authorization headers или raw provider
payloads. Все записи tenant-scoped, approval-bound и idempotent. Kill switch
останавливает новые writes, а MCP не имеет apply boundary. Результаты ошибок
нормализованы и не раскрывают секреты.

## Operational impact

Нужны worker lease/fencing, bounded retry, spend/budget quota, metrics для
unknown/rejection/lag и runbooks для overspend и remote timeout. Откат —
отключение manage capability, включение kill switch и drain worker; immutable
evidence не удаляется.

## Migration and release boundary

Repository synthetic qualification закрывает модель, API, SDK, MCP preview,
UI, persistence, RLS contract и 1 000-SKU scenario. Production claim требует
отдельного credentialed evidence для одного promotion write и одного
advertising write на конкретном marketplace, включая remote read-after-write,
rate limits, partial response и timeout-after-accept. До этого gate состояние
намеренно остаётся `qualification_required`.
