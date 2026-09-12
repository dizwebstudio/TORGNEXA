# Закрытие 234.9 — минимизация OIDC subject в API/UI

Дата проверки: 2026-09-12. Решение:
[ADR-0197](../../adr/0197-oidc-subject-surface-minimization.md).

## Реализованная граница

`WorkspaceMember` больше не содержит стабильный provider subject. API всегда
проецирует только `identity_bound`, UI показывает понятный статус привязки и не
получает raw reference. OpenAPI и сгенерированные SDK обновлены до `0.22.0`;
поле остаётся optional на rolling-upgrade окне, чтобы новый frontend мог
работать со старой API replica.

PostgreSQL-колонка и внутреннее поле repository сохранены для локального JWT,
membership resolution, verified invitation binding и profile lookup. Они не
переходят в management response. Privacy export теперь возвращает тот же
boolean. Restrict/delete/anonymize продолжают выставлять колонку в NULL;
correction не меняет identity binding.

Contract checker отклоняет `oidc_subject` в любом OpenAPI-документе и event
JSON Schema. Это постоянный CI-инвариант, а не одноразовый поиск. API unit test
проверяет сериализацию bound/unbound member. PostgreSQL test дополнительно
проверяет реальные list/update responses, authoritative audit JSON, outbox
payload inventory, export artifact и состояние строки после удаления.

## Совместимость и данные

Удалённое response-поле было optional в `0.21.x`, поэтому старые клиенты должны
были принимать его отсутствие. Новый optional boolean является additive minor
изменением. Новый сервер всегда включает его; старый сервер может не включать
его во время rolling deployment. Generated client operation signatures
остались прежними, обновились API version и source hashes.

Миграция и backfill не нужны. Сам subject классифицирован как pseudonymous
personal identifier и остаётся только в tenant-scoped trusted boundary под
forced RLS. Export не раскрывает его, а delete/anonymize удаляют привязку из
authoritative member row.

## Проверенные сценарии

- bound и unbound member сериализуются как `identity_bound=true/false`;
- list и update не содержат ни internal field name, ни synthetic subject;
- member audit и outbox payload inventory не содержат reference;
- privacy export до удаления содержит `identity_bound=true` без reference;
- delete очищает binding, отключает member, анонимизирует email, а последующий
  export содержит `identity_bound=false`;
- restrict отключает member и очищает binding без анонимизации email;
  retention anonymization очищает binding и заменяет email;
- frontend source regression запрещает прежнее поле и production build
  компилирует новый статус;
- contract regression отклоняет reference в REST и event schemas.

## Результаты

- API/retention unit tests — PASS.
- `./scripts/check-contracts.sh` — PASS.
- `./scripts/check-frontend-shell.sh` — PASS, включая TypeScript и production
  build.
- `./scripts/check-audit-postgres.sh` — PASS с `-race`, disposable PostgreSQL и
  forced RLS.
- `./scripts/check.sh` — PASS: Go test/vet, contracts, generated SDK drift,
  architecture, migrations, supply-chain policy, frontend, package index и
  build.
