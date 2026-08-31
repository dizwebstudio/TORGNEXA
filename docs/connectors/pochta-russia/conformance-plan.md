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
