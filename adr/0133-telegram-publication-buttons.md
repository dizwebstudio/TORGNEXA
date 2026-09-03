# ADR-0133 — HTTPS-кнопки в публикациях Telegram

Status: Accepted

## Context

The Telegram adapter already validates and encodes bounded HTTPS URL buttons,
but the host Social Core stored only text/media variants and the runtime
support contract therefore kept `social.post.buttons` closed. Enabling the
capability only in the manifest would let the UI advertise a side effect that
the API and worker could not carry.

## Decision

Persist an immutable provider-neutral button snapshot on
`social_content_variants`. Accept at most eight buttons, each with a 1–64
character label and an HTTPS URL of at most 2048 bytes. The authenticated
Social API validates the input before creating content, projects the button
capability into a channel only when the connector-account setting is enabled,
and returns the snapshot in publication views. The worker revalidates both the
channel and connector-account capability before mapping to the existing
Telegram adapter.

Admit `social.post.buttons` for Telegram only. Keep callback-data buttons,
inbound updates, edit/delete and MAX buttons outside this admission until
their own authorization and lifecycle contracts are implemented.

## Alternatives considered

Хранить кнопки только в provider-specific payload было отклонено: это ломало
immutable provider-neutral snapshot и API read-after-write semantics.

## Consequences

Telegram получает bounded HTTPS buttons через тот же approval, capability и
receipt lifecycle; callback-data и неподтверждённые действия остаются закрыты.

## Migration and data impact

Миграция добавляет только default-empty JSONB snapshot; существующие строки и
публикации сохраняют прежнее поведение.

## Security and privacy impact

URLs are restricted to HTTPS and the database check rejects extra JSON keys,
control/unsafe URL characters and oversized values. The migration adds a
default-empty JSONB column and extends the existing capability validator; old
readers can ignore the new field. Button data is not copied to events or
secrets. Ambiguous Telegram writes continue to use the existing receipt
recovery path and are never blind-retried.

## Compatibility impact

Старые API clients могут не передавать buttons и продолжают работать с теми же
text/media variants и publication receipts.

## Operational impact

Capability и account setting можно отключить для остановки новых button
publications без удаления уже сохранённых immutable snapshots.
