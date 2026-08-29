# Task 157 — М.Видео и Lamoda в каталоге маркетплейсов

Status: Repository implementation complete

## Scope

- добавить SDK manifests, presentation и branded cards для `mvideo` и `lamoda`;
- зарегистрировать провайдеры в architecture policy и generated catalogs;
- добавить отдельную marketplace health-check surface с tenant-scoped API key
  enrollment и операторским HTTPS probe;
- обновить runtime support, UI, documentation, conformance evidence и tests;
- не выдавать неподтверждённые product/order/price/inventory операции.

## Acceptance criteria

- в «Интеграции → Маркетплейсы» видны карточки «М.Видео» и «Lamoda»;
- для каждой карточки можно сохранить credentials, указать probe URL и
  выполнить bounded проверку подключения;
- `health_only` запрещает capabilities и generic product synchronization;
- policy, manifests, generated Go/TypeScript catalogs, docs and conformance
  reports согласованы;
- contract checks, Go tests/vet, frontend tests/build проходят;
- включение доменных операций требует отдельного provider-qualification review.

## Qualification boundary

Lamoda Seller API и партнёрский API М.Видео могут отличаться по договору,
scopes, endpoint и лимитам. До получения актуальных synthetic fixtures и
тестовых кабинетов runtime намеренно не считает эти операции production-ready.
