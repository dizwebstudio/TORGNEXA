# ADR-0160 — terminal-to-terminal создание отправления «Деловых Линий»

Status: Accepted

## Context

Официальный `POST /v2/request.json` «Деловых Линий» принимает два варианта
доставки: адресный и терминальный. В терминальном варианте remote references
терминала отправления и терминала получения передаются отдельно как
`derival.terminalID` и `arrival.terminalID`. Нейтральный shipment command уже
имеет `pickup_point_ref`, а runtime-конфигурация должна хранить значения,
которые нельзя вывести из адреса или ПВЗ.

## Decision

Сохранить существующую capability `logistics.shipment.create` и допустить для
«Деловых Линий» только bounded terminal-to-terminal вариант поверх текущего
approval-bound worker. `sender_terminal_id` задаётся в tenant-scoped runtime
configuration, а числовой `pickup_point_ref` задаёт терминал получателя.

При наличии `pickup_point_ref` host требует оба числовых remote references,
выбирает `variant=terminal` для derival и arrival, не отправляет адресные
объекты и не меняет остальные ограничения: до 50 мест, явная конфигурация
контрагента/груза/даты/окна, временная login-сессия и отсутствие автоматического
повтора после неоднозначного результата. При пустом `pickup_point_ref` остаётся
прежний address-to-address payload.

## Security and privacy impact

Appkey, PAT и sessionID остаются callback-scoped. В terminal payload уходят
только явные tenant configuration, terminal references и данные заказа,
необходимые официальному endpoint; адресные поля в этом режиме исключаются.
Числовая валидация remote references не разрешает подменять ими внутренние
warehouse IDs.

## Compatibility impact

Изменение аддитивное: capability и нейтральный API не меняются, а
`pickup_point_ref` получает уже предусмотренную Dellin-specific семантику.
Другие перевозчики сохраняют собственные правила обработки этого поля.

## Migration and data impact

Миграция не требуется. Новая настройка хранится в существующем runtime config
аккаунта; для адресных отправлений она необязательна.

## Operational impact

Оператор сначала получает терминалы через bounded directory read, затем указывает
ID отправителя в runtime config и ID получателя в форме создания. Terminal cancel,
гибридные маршруты и возвраты остаются отдельными qualification boundaries.

## Alternatives considered

Выводить терминал отправления из `from` или использовать `pickup_point_ref` для
обоих концов отвергнуто: эти значения являются разными provider-owned remote
references. Передавать пустые адресные объекты отвергнуто: terminal payload
должен быть однозначным и не содержать адресный variant.

## Consequences

«Деловые Линии» теперь поддерживают два проверяемых bounded сценария создания
отправления, не расширяя capability surface и не ослабляя approval, secret,
tenant или idempotency boundaries.
