# Task 156 — Категорийные runtime-поверхности для оставшихся интеграций

Status: Repository implementation complete

## Problem

В каталоге оставались 14 SDK-коннекторов в статусе `planned`, хотя для них
можно безопасно завести tenant-scoped кабинет и проверить credentials. Кроме
того, карточки не были сгруппированы по понятным пользовательским категориям.

## Scope

- перевести 14 записей из `planned` в `separate_surface`;
- сгруппировать их по поверхностям «Объявления и вертикали», «Социальные
  сети», «ЭДО» и «Госсистемы»;
- добавить schema-backed флаг `health_only` и сгенерированные Go/TypeScript
  проекции;
- разрешить account configuration и enablement только для credential + health
  check, оставив operational capabilities и sync пустыми;
- добавить host-mediated bounded catalog probe с fail-closed конфигурацией;
- показать в UI категорию, число карточек и явную границу health-only режима;
- синхронизировать матрицу интеграций, публичную документацию, backlog,
  execution plan и validation evidence.

## Providers

| Surface | Providers |
|---|---|
| Объявления и вертикали | Auto.ru, Avito, CIAN |
| Социальные сети | Instagram, Odnoklassniki, Rutube, Threads, VK, YouTube |
| ЭДО | Diadoc, Saby EDO |
| Госсистемы | Chestny ZNAK, EGAIS, VetIS/Mercury |

## Acceptance criteria

- generated runtime support содержит 61 коннектор: 18 `ready`, 43
  `separate_surface`, 0 `planned`;
- каждая из 14 карточек видна только в своей категорийной поверхности;
- account creation, credentials и authenticated health check работают через
  существующий connector-account контур;
- никакая из 14 записей не получает product, publication, social, document,
  regulated-write или sync capability;
- неизвестные placeholders, произвольные hosts, не-HTTPS URLs и невалидные
  ответы probe отклоняются или нормализуются в degraded health;
- генератор, контракт, Go tests/vet, frontend tests/build и documentation
  checks проходят.

## Qualification boundary

Задача закрывает каталог, onboarding и проверку подключения. Полноценные
публикация, товарная синхронизация, социальные сообщения и ЭДО/госсистемные
операции не заявляются до отдельного provider qualification с реальным
непродуктивным кабинетом, официальным endpoint, worker bridge,
идемпотентностью и retained conformance evidence.
