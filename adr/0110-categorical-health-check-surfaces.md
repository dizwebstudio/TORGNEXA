# ADR 0110: Категорийные health-check поверхности для оставшихся коннекторов

Status: Accepted

## Context

После включения runtime-truthful каталога 14 SDK-коннекторов оставались в
состоянии `planned`. Это смешивало две разные вещи: отсутствие готового
доменного worker-маршрута и невозможность завести кабинет/проверить доступ.
Одновременно карточки без категории было трудно найти в каталоге интеграций.

## Decision

Все 14 записей переводятся в `separate_surface` и группируются по назначению:

- `classified`: Auto.ru, Avito, CIAN;
- `social`: Instagram, Odnoklassniki, Rutube, Threads, VK, YouTube;
- `edo`: Diadoc, Saby EDO;
- `government`: Chestny ZNAK, EGAIS, VetIS/Mercury.

Для этих записей вводится флаг `health_only`. Он разрешает tenant-scoped
создание кабинета, сохранение credentials через SecretProvider и
аутентифицированную проверку подключения, но запрещает любые операционные
capabilities и sync-направления. Провайдеры с каноническим manifest probe
используют его; для SDK без безопасного фиксированного endpoint оператор
задаёт HTTPS `probe_url` в непубличной runtime-конфигурации. Probe работает
через существующую host-mediated транспортную границу, ограничивает хост,
таймаут и размер ответа и нормализует ошибки без утечки credentials.

## Consequences

Каталог больше не содержит planned-записей и показывает все коннекторы на
правильных категорийных поверхностях. Это закрывает discovery, account
enrollment и health-check контур, но не выдаёт ложное обещание публикации,
синхронизации товаров, регулируемых документов или сообщений. Такие
операции появятся только после отдельной квалификации доменного bridge,
worker-маршрута, conformance evidence и policy review.

API и worker сохраняют default-deny: пустой набор capabilities допускается
только для `health_only`, а попытка включить доменную capability по-прежнему
отклоняется. Изменение не требует миграции БД и обратно совместимо с
существующими connector accounts.

## Compatibility impact

Connector SDK v1, public REST/OpenAPI contracts, event envelopes and database
schemas remain unchanged. The additive `health_only` field is consumed only by
the generated runtime/catalog projections and does not change existing ready
connector capability IDs.

## Migration and data impact

No migration is required. Existing connector-account, secret-reference and
health-history rows are reused. An operator may disable or remove a
health-only account through the existing control plane; no credentials are
copied into the build-time contract.

## Security and privacy impact

Credentials stay in the tenant-scoped SecretProvider callback and never enter
the catalog, logs, events or probe error text. Dynamic probes are HTTPS-only,
host-validated, bounded by timeout/body limits and reject unknown placeholders;
no private-network or arbitrary callback target is permitted.

## Operational impact

Operators see all 14 providers in their category tabs and can distinguish a
successful credential health check from a qualified domain integration. A
healthy card is not a go-live claim: publication, synchronization, document
and regulated workflows still require separate provider qualification evidence.

## Alternatives considered

Оставить записи в `planned` было отвергнуто: это скрывало доступный
credential/health контур и нарушало категорийную навигацию. Объявить все SDK
выполненными интеграциями также отвергнуто: наличие пакета не доказывает
рабочий host bridge и worker dispatch. Реализация 14 полноценных доменных
маршрутов вынесена в отдельные provider-qualification задачи, чтобы не
создавать фиктивную production готовность.
