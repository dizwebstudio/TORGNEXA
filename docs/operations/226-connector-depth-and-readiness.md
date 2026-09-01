# Глубина интеграций и readiness: эксплуатация Task 226

## Что проверяется

Матрица строится скриптом
`scripts/generate-connector-readiness.py` из трёх источников:

- 61 `connectors/**/manifest.json`;
- `contracts/connectors/builtin-runtime-support-v1.json`;
- redacted `docs/connectors/*/conformance-report.json` и, если есть,
  `live-qualification-status.json`.

В matrix не попадают токены, Authorization headers, сертификаты, raw HTTP
ответы, персональные данные или секретные fixture-значения.

Текущая сводка: 18 `ready`, 14 `read_only`, 11 `partially_supported`, 16
`health_only` и 2 `manifest_only`. В исходном описании Task 226 было указано
17 health-only; authoritative runtime catalog сейчас содержит 16 явных
`HealthOnly`. `ozon-delivery` и `ozon-pay` остаются двумя
`manifest_only` специализированными поверхностями, потому что для них нет
generic runtime operation surface.

## Как читать статусы

| Статус | Значение | Разрешённая реакция |
|---|---|---|
| `manifest_only` | Есть описание, runtime surface не допущена | не создавать account operation; принять решение о surface/retire |
| `health_only` | Разрешены account lifecycle и health probe | не показывать бизнес-запись |
| `read_only` | Есть безопасный read runtime | синхронизировать только разрешённые чтения |
| `partially_supported` | Есть ограниченный набор операций | показывать capability-level ограничения |
| `ready` | Repository runtime и базовый conformance admission пройдены | применять обычные account/policy/approval gates |
| `qualified` | Exact capability подтверждена credentialed sandbox/live evidence | разрешать production-контур по release policy |
| `degraded` | Runtime или evidence неполны | остановить новые side effects, оставить evidence |
| `reauthorization_required` | Доступ/скоуп истёк или отозван | переподключить через SecretProvider |
| `not_available` | Поверхность не поддерживается | не ретраить операцию вслепую |

`ready` не означает, что внешний кабинет принял запись. Timeout после
удалённого принятия остаётся `unknown` и требует reconciliation. Только
credentialed exact-capability evidence может перевести профиль в `qualified`.

## API, SDK, MCP и frontend

Read-only API:

- `GET /api/v1/connector-readiness?limit=&cursor=&family=&surface=&status=&priority=`;
- `GET /api/v1/connector-readiness/{connector_id}`.

Оба маршрута tenant-authorized permission
`integrations.center.read`, не делают remote calls и возвращают только
redacted catalog data. Пагинация стабильна по `connector_id`. Generated Go,
Python и TypeScript SDK получают те же операции. MCP предоставляет
`commerce.connectors.readiness.list` только для чтения и не принимает
credentials или tenant selectors.

В Integration Center отображается отдельный каталог всех 61 коннектора:
уровень, число read/write capabilities, owner/priority, blocker и следующий
шаг. Кнопки удалённой записи readiness-таблица не создаёт; команды проходят
через существующие policy, approval, account-health, SecretProvider и
reconciliation gates.

## Qualification waves и инциденты

1. Core commerce: marketplace/storefront catalog, prices, inventory, orders.
2. Logistics: shipment, label, tracking и returns.
3. Finance/compliance: payments, EDO и government operations.
4. Identity, notifications, AI/social и specialized surfaces.

Для каждой волны сохраняются connector version, SDK version, exact scopes,
environment, deterministic conformance, sandbox/live result и timestamp.
Credentialed evidence хранится вне Git в защищённом release/CI artifact
контуре; в репозитории остаётся только redacted summary.

При отзыве токена сначала отключается capability или kill switch, затем
очищается runtime cache и запускается reauthorization. При rate limit или
outage новые операции получают retry/DLQ согласно connector policy. При
`unknown` не выполняется слепной повтор: создаётся reconciliation finding.
Canonical inventory, payment и settlement ledgers не переписываются.

Проверка локального gate:

```bash
make connector-readiness-qualification
```

Этот gate подтверждает инвентарь, обязательные conformance reports,
несекретность evidence и запрет `qualified` без live evidence. Реальная
проверка с доступом к кабинету является отдельным внешним release-gate.
