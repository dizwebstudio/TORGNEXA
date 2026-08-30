# ADR 0122: Исполнение маркировки, агрегации и УПД

## Status

Accepted for Epic 171. Реальные production-провайдеры допускаются только
после отдельной qualification-проверки.

## Context

В репозитории уже есть read-only статусы Честного знака, изолированная УКЭП/
МЧД-подпись, EDO SDK и WMS. Это полезные основания, но они не образуют
полный процесс: получение кодов, печать, сканирование, упаковка, УПД,
ввод/вывод и reconciliation. Нельзя добавлять эти операции как набор
provider-specific HTTP-вызовов: юридически значимый результат может быть
принят удалённой системой после timeout.

## Decision

1. Ввести `internal/core/marking` как provider-neutral домен. Он хранит
   только fingerprint кода, GTIN/SKU, локальный статус, package graph,
   operation receipt, печатное задание, скан, документ и remote observation.
2. Сырые коды разрешены только в callback-контуре короткоживущего
   `RawCodeStore`. В PostgreSQL, outbox/inbox, audit summary, логах, SDK
   result и обычном API присутствуют только fingerprint и opaque artifact
   reference с TTL.
3. Любая удалённая запись имеет idempotency key, dry-run режим и состояния
   `succeeded`, `failed`, `unknown`. Timeout после отправки переводит работу в
   `unknown` и запускает чтение/reconciliation, а не слепой повтор.
4. Capability registry расширяется шестью typed operations:
   `marking.codes.request`, `marking.codes.reserve`,
   `marking.aggregation.write`, `marking.circulation.introduce`,
   `marking.circulation.withdraw`, `marking.transfer.write`. Все remote writes
   остаются `write_sensitive` и требуют approval.
5. Иерархия упаковки — `unit → kit → box → pallet`. Циклы, повторное
   использование кода, закрытие с неполным составом и расхождение количества
   отклоняются до remote write; разбор упаковки — отдельная операция с
   evidence.
6. УПД — versioned artifact с форматом 5.03. Строки связываются с GTIN/SKU,
   fingerprints и package refs; подпись и МЧД проходят существующий
   isolated signing service, отправка — существующий EDO порт. Provider
   transport не получает private key.
7. Полный процесс является оркестрацией доменных команд, а не второй
   state-machine провайдера: `codes → print → scan → aggregate → reserve →
   UPD → sign → EDO → circulation → reconciliation`.
8. В runtime admission Честный знак, Diadoc, Saby, KKT/OFD и marketplace
   writes остаются qualification-gated до официальных тестовых реквизитов,
   детерминированных fixtures, idempotency/retry evidence и проверки
   юридического результата.

## Operation matrix

| Операция | Локальная проверка | Remote capability | Approval | После timeout |
|---|---|---|---|---|
| Получить коды | GTIN, группа, количество, snapshot | `marking.codes.request` | да | `unknown` + status read |
| Зарезервировать/отменить | batch, доступный остаток | `marking.codes.reserve` | да | `unknown` + quantity reconciliation |
| Напечатать | template version, printer ref, one-use guard | edge adapter | нет, если локальная печать | print job `unknown` |
| Сканировать | fingerprint, GTIN/SKU, WMS action, count | — | по WMS policy | rejected/duplicate/overflow |
| Агрегировать | parent/child graph, cycle, composition, close | `marking.aggregation.write` | да | `unknown` + package read |
| Ввести в оборот | document, approval, signature/MChD | `marking.circulation.introduce` | да | `unknown` + remote status |
| Вывести из оборота | reason, document, approval, signature/MChD | `marking.circulation.withdraw` | да | `unknown` + remote status |
| Передать | from/to location, document, codes | `marking.transfer.write` | да | `unknown` + remote status |
| УПД/ЭДО | format 5.03, lines, signature, MChD | `edo.documents.send` | да | EDO status read |
| Reconciliation | local/remote facts, quantity and graph | `marking.status.read` | запуск policy | drift/manual attention |

## Alternatives considered

- Хранить полный код в `marking_codes`: отклонено из-за риска утечки в
  резервные копии, логи, события и обычные ответы API.
- Выполнять remote write прямо из HTTP handler: отклонено; worker и outbox
  должны переживать retry, lease loss и неизвестный результат.
- Выдать production capability вместе с manifest: отклонено; для
  юридически значимых операций нужен отдельный non-production qualification.

## Compatibility impact

Новые typed SDK interfaces и OpenAPI routes additive. Существующие
read/status коннекторы, EDO SDK и WMS API не меняют семантику.

## Migration and data impact

Migration `000037_marking_execution.sql` — expand-only, high risk, backup
required. Existing read/status tables не переписываются. Новые таблицы имеют
composite tenant keys, FORCE RLS, append-only evidence triggers and bounded
references. Public API и SDK additive.

## Security and privacy impact

Tenant scope берётся из аутентифицированного контекста. RawCodeStore имеет
короткий TTL и callback-only доступ; SQL, outbox/inbox, audit, logs и API
получают только fingerprint/opaque reference. Private key не передаётся
коннектору.

## Operational impact

Migration требует backup checkpoint. Worker должен возобновлять queued работу
по lease, а `unknown` переводить в reconciliation/manual attention. На малом
VPS печать и сканирование ограничиваются bounded batch/queue; remote retries
подчиняются manifest policy и не повторяются после неизвестного результата.

Для УПД используется формат 5.03 и штатный docflow: seller title, operator
confirmations, buyer receipt/title, rejection/clarification and correction
states. The current format and attachment rules must be verified against the
[официальной документации Диадока по УПД](https://developer.kontur.ru/doc/diadoc-api/docflows/utd.html).
Операции получения кодов, ввода/вывода, возврата, списания, упаковки,
передачи и ЭДО сверяются с [официальной документацией ГИС МТ](https://docs.crpt.ru/gismt/).

## Consequences

Появляется единый проверяемый контур для WMS, ЭДО и госсистемы без утечки
кодов. Он готов к synthetic/demo qualification и безопасно закрывает
repository-level work, но live provider readiness по-прежнему является
отдельным gate, а не следствием наличия manifest или typed interface.
