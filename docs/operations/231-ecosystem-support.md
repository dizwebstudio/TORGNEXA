# Task 231: экосистема и поддержка

Документ описывает repository-ready контур для integrations, apps, partners,
mobile, cloud и support. Он не является публичным обещанием SLA или списка
готовых marketplace-записей.

## Где смотреть

- UI: `/ecosystem` — сводка портфеля, видимых приложений, onboarding, partners,
  mobile/hosted surfaces, support и внешних gates.
- API: `GET /api/v1/ecosystem/overview` и
  `GET /api/v1/ecosystem/metrics`.
- Onboarding: `GET/POST /api/v1/ecosystem/onboarding`.
- Certification: `GET/POST /api/v1/ecosystem/partners/certifications`.
- MCP: `commerce.ecosystem.overview` — только tenant-scoped read.
- SDK: методы `GetEcosystemOverview`, `GetEcosystemMetrics`, списки и
  idempotent create-операции сгенерированы из OpenAPI.

Все запросы требуют authenticated organization/workspace context. POST
запросы требуют уникальный `Idempotency-Key`; сначала создаётся audit record,
затем evidence сохраняется в append-only таблице.

## Как читать статусы

`integrated` означает наличие контракта и регистрации, `verified` — прошедшие
репозиторные security/supply-chain/conformance checks, `ready` — runtime
capability с retained evidence, `qualified` — exact credentialed sandbox/live
qualification, `supported` — documented owner, compatibility/version policy и
support response target. `deprecated` и `blocked` запрещают использование.

Нельзя повышать статус по количеству manifest, health-check, SDK или listing.
`qualified` пропускается только с evidence kind `credentialed_sandbox` или
`credentialed_live`; `supported` требует evidence kind `support`. В UI/API
неподтверждённая операция должна оставаться read-only, partial или
qualification-required.

## Повторяемый onboarding

Onboarding принимает bounded список checks. Отсутствующий state становится
`pending`, обязательный failed check переводит run в `blocked`, pending check —
в `running`, а complete без failed/pending — в `ready`. Это готовность
onboarding, а не квалификация connector-а. Повтор с тем же ключом безопасен;
другой payload с тем же ключом получает conflict.

Partner certification хранит только partner reference, tier/state и redacted
evidence. Состояние `certified` требует credentialed evidence и срока действия.
Истёкшие или revoked записи не считаются поддерживаемыми.

## Reuse существующих контуров

Task 231 не создаёт вторые сущности:

- readiness и capability — `connector-readiness`/Connector SDK;
- apps, consent, artifact и revoke — plugin marketplace;
- обращения и SLA-case — customer service/unified inbox;
- WMS scan/pick/pack/print и offline receipts — mobile warehouse;
- hosted subscription — Cloud billing;
- SLI/SLO, backup/restore и DR — SLO/backup-dr контуры;
- Product/Order nodes и signed webhooks — внешний versioned n8n package.

Ecosystem API только агрегирует эти источники и добавляет onboarding/partner
evidence, которого у них нет. Ошибка одного источника не превращается в нулевую
операционную гарантию: overview возвращает ошибку чтения, а внешние claims
остаются qualification-required.

## Repository qualification

```bash
make ecosystem-support-qualification
```

Проверка запускает core/API/MCP тесты, проверяет migration 58 и checksum,
FORCE RLS/append-only/redaction guards, OpenAPI/runtime route parity, SDK,
MCP, frontend, policy и документацию. Synthetic PASS означает, что граница
репозитория собрана и безопасна; это не live certification.

## Внешние release-gates

Перед claim `qualified` или `supported` нужно отдельно сохранить versioned,
redacted evidence для:

1. credentialed WB/Ozon/прочих connector capability — scopes, timeout/unknown,
   retry, write/read-after-write и reconciliation;
2. partner sandbox UAT, cutover и rollback;
3. hosted region/topology, measured SLO, backup restore и DR drill;
4. Android/iOS/handheld camera-scanner/printer device matrix;
5. production support rota, incident/status communication and SLA terms.

Без этих материалов UI честно показывает `qualification_required`; этот
статус нельзя заменить marketing count или количеством манифестов.
