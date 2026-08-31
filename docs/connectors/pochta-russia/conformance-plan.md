# План conformance для «Почты России»

`connectors/logistics/pochta-russia` содержит sandbox-кандидат,
детерминированное чтение тарифов и пунктов выдачи и проверку здоровья без сети
и боевых credentials. Общий harness проверяет SDK v1, границу секретов,
нормализацию ошибок, tenant isolation, egress и sandbox.

Перед включением оформления отправлений, отдельного возвратного отправления и
прочих документов
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
