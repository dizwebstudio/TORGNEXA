# ADR-0152 — редактирование и удаление сообщений MAX

Status: Accepted

## Context

Официальный MAX Bot API поддерживает `PUT /messages` для редактирования и
`DELETE /messages` для удаления сообщений. До этого runtime TORGNEXA
публиковал и читал статус сообщений MAX, но намеренно оставлял удаляющие
операции fail-closed.

## Decision

Добавить в MAX manifest и built-in runtime явные capabilities
`social.post.edit` и `social.post.delete`. Редактор вызывает
`PUT /messages?message_id=...`, а deleter — `DELETE /messages?message_id=...`
на фиксированном `platform-api2.max.ru`. В обоих случаях message ID берётся
только из проверенной immutable remote receipt и сопоставляется с одним
настроенным каналом.

Редактирование повторно проверяет provider-neutral text/media/buttons. Для
media authenticated API и worker непосредственно перед запросом используют
released-upload bridge, который заново проверяет release и передаёт только
opaque attachment token. Ответ принимается только при явном `success=true`.
Удаление также требует явного `success=true`.
API/Core/worker используют существующий approval-bound, tenant-scoped
operation receipt и audit flow; новая база или новый публичный endpoint не
требуются.

## Security and privacy impact

Операции доступны только для активного аккаунта с включённой capability и
заранее одобренного опубликованного сообщения. Чужой канал, произвольный
message ID, секрет или исходный медиафайл не пересекают host boundary.
Неоднозначный write transport нормализуется в `write_outcome_unknown` и не
повторяется автоматически.

## Compatibility impact

Публикация, status read и webhook-контракты не меняются. Capabilities
добавляются аддитивно, а существующие generic Social API edit/delete routes
начинают разрешать MAX только после явной runtime-support и account-capability
проверки. Telegram остаётся совместимым.

## Operational impact

Редактирование и удаление ограничены provider rate limit и текущей
approval/idempotency моделью Social Core. Ошибки прав, канала, сообщения и
явный `success=false` показываются как постоянная ошибка; timeout/5xx не
повторяются вслепую и остаются неизвестным исходом операции.

## Migration and data impact

Миграция не требуется. Используются существующие remote receipts, operation
receipts и audit records; новые durable поля не добавляются.

## Alternatives considered

Оставить MAX edit/delete fail-closed отвергнуто: официальные PUT/DELETE
маршруты задокументированы и позволяют строгую проверку канала и результата.
Создавать MAX-специфичный Core API отвергнуто: provider-neutral Social API уже
имеет approval-bound edit/delete контракт. Разрешить удаление без approval
отвергнуто как небезопасное необратимое действие.

## Consequences

Оператор может изменять текст/медиа/HTTPS-кнопки и удалять одно опубликованное
сообщение MAX из общего интерфейса публикаций. Комментарии, callback actions,
подписка webhook и прочие MAX-операции остаются вне этого admission.
