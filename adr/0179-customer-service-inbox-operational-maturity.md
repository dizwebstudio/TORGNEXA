# ADR-0179: Зрелый контур клиентского сервиса и единого inbox

Status: Accepted

## Context

Tasks 056, 057 и 009 уже содержат claims/disputes, provider-neutral
Conversation/Message/Case/Assignment/SLA и inbox idempotency. Но операторский
сценарий оставался разнесённым: отзывы и вопросы не имели общей очереди,
customer history нельзя было безопасно собрать, а outbound reply не имел
единого durable intent и состояния `unknown`.

## Decision

Ввести provider-neutral Customer Service projection поверх существующих
канонических агрегатов. В неё входят tenant-scoped Conversation, immutable
Message, minimized CustomerRef, Reply intent/receipt, Assignment history,
versioned SLA metadata и reconciliation Findings. Ссылки на Order, Product,
Return и Claim остаются references, а не копиями.

Inbound нормализуется до sanitized text и content digest. Уникальность треда
определяется по organization/workspace/source/account/remote thread, а
сообщения — по remote message identity внутри треда. Повтор webhook или poll
безопасен; конфликт и unknown remote outcome остаются видимыми для
reconciliation.

Customer identity переводится в `verified` только по approved exact reference
(remote customer ID, verified contact token или подтверждённая связь с
заказом). Имена и похожие контакты не являются основанием для объединения.
Customer 360 показывает только минимизированные ссылки и разрешённые
события; raw provider payload, credentials и лишняя PII не сохраняются.

Публичный ответ, внутренняя заметка и AI draft имеют разные visibility/origin
и delivery states. Public reply требует permission, Idempotency-Key и
подходящую capability/approval policy. AI может подготовить draft, но не
может отправить ответ, изменить SLA, закрыть case или подтвердить
компенсацию. Timeout после remote accept переводит intent в `unknown`, а не в
слепой retry.

## Consequences

- Единый inbox поддерживает сообщения, отзывы, вопросы, претензии и возвратные
  обращения без второго CRM или customer master.
- Очередь, assignment, SLA, thread и Customer 360 имеют одинаковую
  tenant/workspace boundary и cursor-safe read API.
- История сообщений, assignment, SLA events, attachments и findings
  append-only; исправление или удаление проходит через redaction/retention
  policy, а не через тихий UPDATE.
- Каналы без exact conformance evidence отображаются как
  `read_only`/`not_available`/`qualification_required`. Локальная проверка
  репозитория не объявляет канал production-qualified.

## Compatibility and migration impact

Изменение аддитивное. Новая migration 000055 создаёт отдельные service
projection tables, foreign-key references к workspace и forced RLS. Existing
Order, Product, Return, Claim, Payment и inbox/outbox contracts не меняются.
OpenAPI и generated SDK расширяются новыми customer-service operations.

## Security and privacy impact

Все новые таблицы tenant-scoped и защищены `FORCE ROW LEVEL SECURITY`.
Принимаются только безопасный текст, digest, masked customer references и
released upload references. Запрещены credentials, Authorization headers, raw
provider payloads, full payment credentials и необязательные customer PII в
логах, событиях, audit и обычных API-ответах. Attachments проходят общую
quarantine/release policy.

## Operational and release boundary

Локальный gate: `make customer-service-qualification`. Он проверяет migration
catalog/hash, OpenAPI/SDK/UI/MCP wiring, RLS, append-only history и secret
boundary. Отдельно на целевой topology выполняются credentialed sandbox/live
checks inbound, reply, attachments, moderation, rate limits,
read-after-write и timeout/unknown recovery с redacted retained evidence.

## Alternatives considered

Создать полноценный CRM внутри customer service отклонено: canonical
customer/order/claims ownership уже существует. Автоматически объединять
клиентов по имени или свободному тексту отклонено из-за privacy и ошибок
идентификации. Считать ответ отправленным после HTTP 2xx отклонено: receipt,
read-after-write и reconciliation необходимы для безопасного outbound.
