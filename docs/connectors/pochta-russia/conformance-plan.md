# План conformance для «Почты России»

`connectors/logistics/pochta-russia` содержит sandbox-кандидат,
детерминированное чтение тарифов и пунктов выдачи и проверку здоровья без сети
и боевых credentials. Общий harness проверяет SDK v1, границу секретов,
нормализацию ошибок, tenant isolation, egress и sandbox.

Перед включением оформления отправлений и прочих документов
необходимо получить тестовый доступ к API «Отправка», зафиксировать текущую
версию REST/SOAP-контрактов, добавить синтетические fixtures для write
операций и проверить безопасное восстановление после неоднозначного ответа.
Для уже созданного RPO bounded `logistics.return.create` использует
`PUT /1.0/returns`; qualification должна подтвердить один `direct-barcode`,
один `return-barcode`, идемпотентность и поведение после timeout.

`logistics.label.read` с форматом `return_pdf` использует
`GET /1.0/forms/{rpo}/easy-return-pdf` с `print-type=PAPER`. Fixture должен
подтвердить domestic/S10 barcode, auth headers, print type, PDF media
type/signature, content-addressed artifact reference и отказ на
не-PDF/невалидный RPO. Live qualification должна также проверить наличие формы
для реального тестового RPO.

Для формата `batch_f103_pdf` fixture должен проверить `GET
/1.0/forms/{batch-name}/f103pdf`, числовой номер партии, заголовки авторизации,
`application/pdf`, сигнатуру `%PDF-`, opaque artifact reference и отказ на
нечисловой номер или не-PDF. Live qualification должна использовать партию из
тестового кабинета.

Для `formed_order_pdf` fixture должен проверить `GET
/1.0/forms/{order-id}/forms`, один числовой order ID, `sending-date`,
`print-type=PAPER`, заголовки авторизации, `application/pdf`, сигнатуру `%PDF-`,
opaque artifact reference и отказ на нечисловой ID или не-PDF. Live
qualification должна использовать заказ, уже включённый в тестовую партию.

Для `logistics.batches.read` fixture должен вернуть две партии через
`GET /1.0/batch`, проверить заголовки авторизации, `mailType`/`mailCategory`,
`size` и `page`, а также нормализацию `batch-name`, `batch-status` и
`shipment-count`. Harness обязан отклонять дубликаты, отрицательное или
нечисловое количество и ответ, превышающий запрошенный размер; состав заказов
и операции формирования/передачи партии в fixture не входят.

Для `logistics.return.separate.create` fixture должен проверить ровно один
элемент через `PUT /1.0/returns/return-without-direct`, преобразование
адресов/объявленной ценности, `position=0`, `return-barcode`, заголовки
авторизации и отказ на ошибки/невалидный штрихкод. Live qualification должна
подтвердить, что повтор того же operation receipt не вызывает вторую заявку.

Для `logistics.batches.archive` fixture должен проверить `PUT /1.0/archive`,
массив с одним числовым именем партии, заголовки авторизации и точное
совпадение единственного `batch-name` в ответе. Harness обязан отклонять
нечисловой или другой номер партии, пустой/ошибочный ответ и любой
`error-code`. Live qualification должна подтвердить повторяемость через
tenant-scoped operation receipt и обратимость операции через отдельный
revert-маршрут.

Для `logistics.batches.unarchive` fixture должен проверить `POST
/1.0/archive/revert`, массив с одним числовым именем партии, заголовки
авторизации и точное совпадение единственного `batch-name` в ответе с
`RESTORED`/`archived=false`. Harness обязан отклонять нечисловой или другой
номер партии, пустой/ошибочный ответ и любой `error-code`. Live qualification
должна подтвердить повторяемость через tenant-scoped operation receipt и
согласованную пару archive/revert на тестовой партии.

Для `logistics.batches.archive.read` fixture должен проверить `GET
/1.0/archive`, заголовки авторизации и нормализацию только идентификатора,
статуса и количества отправлений. Harness обязан отклонять дубликаты,
нечисловые/отрицательные значения количества, невалидный статус и ответ,
превышающий host-лимит 100 записей; строки заказов не должны пересекать
границу коннектора.

Для `logistics.return.separate.delete` fixture должен проверить `DELETE
/1.0/returns/delete-separate-return?barcode=...`, отсутствие тела, заголовки
`Authorization`/`X-User-Authorization` и точную передачу ШПИ. Успешным
считается только `2xx` с пустым телом или JSON с пустым `code`; любой код
ошибки (`RETURN_SHIPMENT_NOT_FOUND`, `ILLEGAL_RETURN_SHIPMENT_STATE` и др.)
должен оставаться ошибкой. Host обязан отклонять невалидный barcode,
сохранять только `DELETED`/`deleted=true` и не повторять внешний вызов при
повторе того же tenant-scoped operation receipt. Live qualification должна
использовать исключительно тестовый возврат и подтвердить необратимость,
timeout/reconciliation и approval boundary.

Для `logistics.orders.restore` fixture должен проверить `POST /1.0/user/backlog`,
массив числовых order IDs, оба заголовка авторизации и нормализацию полного
набора `result-ids` в `restored`. Harness обязан отклонять любой `errors`,
частичный или переставленный с дубликатами результат, а также нечисловые ID.
Live qualification должна подтвердить, что операция возвращает заказ из
сформированной партии в «Новые», не является отменой и защищена
tenant-scoped operation receipt.

Для `logistics.return.separate.edit` fixture должен проверить `POST
/1.0/returns/{barcode}`, заголовки авторизации, отсутствие caller-controlled
URL и полный ограниченный JSON payload. Успешным считается только ответ с
тем же `return-barcode` (в объекте или единственном элементе массива) без
`errors`; пустой ответ, другой ШПИ или код ошибки должны оставаться ошибкой.
Host сохраняет только `UPDATED`/`updated=true`, а live qualification
выполняется на disposable-тестовом возврате с проверкой idempotency,
approval и timeout/reconciliation.

Для `logistics.batches.orders.read` fixture должен проверить `GET
/1.0/batch/{batch-name}/shipment`, числовой batch ID, `size`, `page`,
`sort=ask` и оба заголовка авторизации. Harness обязан нормализовать два
заказа в безопасную проекцию, привести статус к lowercase, отклонить
несовпадающую партию, дубликаты, невалидные ID/barcode/status и ответ сверх
лимита. Fixture с полями получателя и адреса должен подтвердить, что эти поля
не пересекают границу коннектора. Live qualification выполняется только на
тестовой партии и не добавляет запись или изменение состояния у провайдера.

Для `logistics.orders.read` fixture должен проверить `GET
/1.0/shipment/{id}` без query/body, числовой order ID и оба заголовка
авторизации. Harness должен принять object или массив ровно с одной записью,
сверить ID запроса и ответа, нормализовать uppercase status в lowercase и
отклонить другой ID, отсутствие batch ID, malformed barcode/status и массив с
несколькими строками. Поля получателя/адреса в fixture не должны попасть в
проекцию. Live qualification выполняется на тестовом заказе в партии и не
изменяет состояние провайдера.

Для точечного lookup партии fixture должен проверить `GET
/1.0/batch/{batch-name}` без query/body, числовое имя, auth headers и exact
`batch-name` в ответе. Допускается object или массив ровно с одним объектом;
несовпадение, несколько объектов, malformed status/count и provider error
должны отклоняться. Projection ограничена ID, статусом, количеством
отправлений и UTC observation time; строки заказов не пересекают границу.

Для `logistics.orders.search` conformance fixture должен проверить
`GET /1.0/backlog/search` с query `query`, auth headers и ограничением
`limit<=100` на стороне host. Проверяются точное совпадение merchant order
number, нормализация статуса, optional batch/barcode, duplicate rejection и
отсутствие PII/raw payload в проекции. Ответ с другим номером, malformed
reference/status или превышением лимита остаётся provider failure. Live
qualification read-only и не изменяет состояние заказа.

Для `logistics.batches.sending_date.write` fixture должен проверить
`POST /1.0/batch/{batch-name}/sending/YYYY/MM/DD`, отсутствие query/body,
числовую партию, корректное разбиение ISO-даты на path-сегменты и оба
заголовка авторизации. Нужно принять пустой `2xx` или JSON без `error-code`,
но отклонить provider error, malformed body, другой batch ID и неверную дату.
Live qualification проводится на disposable сформированной партии с заранее
одобренным запросом, проверкой operation receipt, повтором того же ключа и
сверкой поведения после timeout; боевые партии и реальные клиентские данные
не используются.
